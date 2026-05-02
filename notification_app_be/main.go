package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	baseURL    = "http://20.207.122.201/evaluation-service"
	plannerURL = "http://localhost:8081/plan"
)

// ---------------- GLOBALS ----------------

var (
	bearerToken  string
	tokenMu      sync.RWMutex
	notifications []Notification
	notifMu      sync.RWMutex
)

// ---------------- TOKEN HELPERS ----------------

func getToken() string {
	tokenMu.RLock()
	defer tokenMu.RUnlock()
	return bearerToken
}

func setToken(t string) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	bearerToken = t
}

// ---------------- STRUCTS ----------------

type AuthRequest struct {
	Email        string `json:"email"`
	Name         string `json:"name"`
	RollNo       string `json:"rollNo"`
	AccessCode   string `json:"accessCode"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
}

type AuthResponse struct {
	TokenType   string `json:"token_type"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type LogPayload struct {
	Stack   string `json:"stack"`
	Level   string `json:"level"`
	Package string `json:"package"`
	Message string `json:"message"`
}

// FINAL NOTIFICATION FORMAT
type Notification struct {
	ID        string `json:"ID"`
	Type      string `json:"Type"`
	Message   string `json:"Message"`
	Timestamp string `json:"Timestamp"`
}

// Planner Response

type SelectedVehicle struct {
	ID                     string `json:"id"`
	ServiceDurationInHours int    `json:"serviceDurationInHours"`
	OperationalImpactScore int    `json:"operationalImpactScore"`
}

type DepotResult struct {
	DepotID             string            `json:"depotId"`
	MechanicHoursBudget int               `json:"mechanicHoursBudget"`
	TotalHoursUsed      int               `json:"totalHoursUsed"`
	MaxImpactScore      int               `json:"maxImpactScore"`
	SelectedVehicles    []SelectedVehicle `json:"selectedVehicles"`
}

type PlanResponse struct {
	Results []DepotResult `json:"results"`
}

// ---------------- HTTP CLIENT ----------------

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

// ---------------- MIDDLEWARE ----------------

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func LoggingMiddleware() gin.HandlerFunc {

	return func(c *gin.Context) {

		start := time.Now()

		bodyBytes, _ := io.ReadAll(c.Request.Body)

		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		log.Printf("REQUEST: %s %s",
			c.Request.Method,
			c.Request.URL.Path,
		)

		log.Printf("BODY: %s", string(bodyBytes))

		blw := &bodyLogWriter{
			body:           bytes.NewBufferString(""),
			ResponseWriter: c.Writer,
		}

		c.Writer = blw

		c.Next()

		duration := time.Since(start)

		log.Printf("STATUS: %d", c.Writer.Status())
		log.Printf("TIME: %s", duration)
		log.Printf("RESPONSE: %s", blw.body.String())

		if tok := getToken(); tok != "" {

			go sendLog(
				"backend",
				"info",
				"middleware",
				"Request to "+c.Request.URL.Path+" completed",
			)
		}
	}
}

// ---------------- HELPERS ----------------

func proxyRequest(
	method string,
	url string,
	body []byte,
	token string,
) (int, []byte, error) {

	req, err := http.NewRequest(
		method,
		url,
		bytes.NewBuffer(body),
	)

	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if token != "" {
		req.Header.Set(
			"Authorization",
			"Bearer "+token,
		)
	}

	resp, err := httpClient.Do(req)

	if err != nil {
		return 0, nil, err
	}

	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, respBody, nil
}

func generateID() string {
	return uuid.New().String()
}

// ---------------- AUTH ----------------

func authHandler(c *gin.Context) {

	var req AuthRequest

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(400, gin.H{
			"error": err.Error(),
		})

		return
	}

	jsonData, _ := json.Marshal(req)

	status, body, err := proxyRequest(
		"POST",
		baseURL+"/auth",
		jsonData,
		"",
	)

	if err != nil {

		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	var authResp AuthResponse

	if err := json.Unmarshal(body, &authResp); err == nil &&
		authResp.AccessToken != "" {

		setToken(authResp.AccessToken)

		log.Println("TOKEN SAVED")

		c.JSON(status, authResp)

	} else {

		c.Data(status, "application/json", body)
	}
}

// ---------------- GENERATE NOTIFICATIONS ----------------

func generateNotificationsHandler(c *gin.Context) {

	tok := getToken()

	if tok == "" {

		c.JSON(401, gin.H{
			"error": "authenticate first",
		})

		return
	}

	req, err := http.NewRequest(
		"GET",
		plannerURL,
		nil,
	)

	if err != nil {

		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	req.Header.Set(
		"Authorization",
		"Bearer "+tok,
	)

	resp, err := httpClient.Do(req)

	if err != nil {

		c.JSON(500, gin.H{
			"error": err.Error(),
		})

		return
	}

	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var plan PlanResponse

	if err := json.Unmarshal(body, &plan); err != nil {

		c.JSON(500, gin.H{
			"error": "failed to parse planner response",
		})

		return
	}

	notifMu.Lock()

	newNotifications := []Notification{}

	for _, result := range plan.Results {

		n := Notification{
			ID:        generateID(),
			Type:      "Result",
			Message:   formatMessage(result),
			Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		}

		notifications = append(notifications, n)
		newNotifications = append(newNotifications, n)
	}

	notifMu.Unlock()

	go sendLog(
		"backend",
		"info",
		"notifications",
		"Notifications generated",
	)

	c.JSON(200, gin.H{
		"notifications": newNotifications,
	})
}

// ---------------- GET NOTIFICATIONS ----------------

func getNotificationsHandler(c *gin.Context) {

	notifMu.RLock()

	defer notifMu.RUnlock()

	c.JSON(200, gin.H{
		"notifications": notifications,
	})
}

// ---------------- CLEAR NOTIFICATIONS ----------------

func clearNotificationsHandler(c *gin.Context) {

	notifMu.Lock()

	notifications = []Notification{}

	notifMu.Unlock()

	c.JSON(200, gin.H{
		"message": "notifications cleared",
	})
}

// ---------------- MESSAGE FORMATTER ----------------

func formatMessage(r DepotResult) string {

	return "Depot " + r.DepotID +
		" scheduled with impact score " +
		itoa(r.MaxImpactScore)
}

// ---------------- INTEGER TO STRING ----------------

func itoa(n int) string {

	if n == 0 {
		return "0"
	}

	result := ""

	for n > 0 {

		result = string(rune('0'+n%10)) + result

		n /= 10
	}

	return result
}

// ---------------- SEND LOG ----------------

func sendLog(
	stack string,
	level string,
	pkg string,
	message string,
) {

	tok := getToken()

	if tok == "" {
		return
	}

	payload := LogPayload{
		Stack:   stack,
		Level:   level,
		Package: pkg,
		Message: message,
	}

	jsonData, _ := json.Marshal(payload)

	status, body, err := proxyRequest(
		"POST",
		baseURL+"/logs",
		jsonData,
		tok,
	)

	if err != nil {

		log.Println("LOG ERROR:", err)

		return
	}

	log.Printf(
		"LOG RESPONSE [%d]: %s",
		status,
		string(body),
	)
}

// ---------------- MAIN ----------------

func main() {

	r := gin.Default()

	r.Use(LoggingMiddleware())

	// AUTH
	r.POST("/auth", authHandler)

	// NOTIFICATIONS
	r.POST(
		"/notifications/generate",
		generateNotificationsHandler,
	)

	r.GET(
		"/notifications",
		getNotificationsHandler,
	)

	r.DELETE(
		"/notifications",
		clearNotificationsHandler,
	)

	log.Println(
		"🚀 Notification System running on :8080",
	)

	r.Run(":8080")
}