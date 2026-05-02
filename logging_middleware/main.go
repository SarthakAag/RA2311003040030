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
)

const (
	baseURL = "http://20.207.122.201/evaluation-service"
)

var (
	bearerToken string
	tokenMu     sync.RWMutex
)

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


type RegisterRequest struct {
	Email          string `json:"email"`
	Name           string `json:"name"`
	MobileNo       string `json:"mobileNo"`
	GithubUsername string `json:"githubUsername"`
	RollNo         string `json:"rollNo"`
	AccessCode     string `json:"accessCode"`
}

type RegisterResponse struct {
	Email        string `json:"email"`
	Name         string `json:"name"`
	RollNo       string `json:"rollNo"`
	AccessCode   string `json:"accessCode"`
	ClientID     string `json:"clientID"`
	ClientSecret string `json:"clientSecret"`
}

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



func main() {
	r := gin.Default()
	r.Use(LoggingMiddleware())

	r.POST("/register", registerHandler)
	r.POST("/auth", authHandler)

	log.Println("🚀 Server started at :8080")
	r.Run(":8080")
}

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}


func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		bodyBytes, _ := io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		log.Printf("=== REQUEST: %s %s", c.Request.Method, c.Request.URL.Path)
		log.Printf("=== BODY: %s", string(bodyBytes))

		// Capture response body
		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		duration := time.Since(start)
		log.Printf("=== STATUS: %d | TIME: %s", c.Writer.Status(), duration)
		log.Printf("=== RESPONSE BODY: %s", blw.body.String())

		if tok := getToken(); tok != "" {
			go sendLog("backend", "info", "middleware", "Request to "+c.Request.URL.Path+" completed in "+duration.String())
		} else {
			log.Println("SKIPPING LOG: no token yet")
		}
	}
}

// bodyLogWriter captures response body for logging
type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}


func proxyRequest(method, url string, body []byte, token string) (int, []byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}



func registerHandler(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	jsonData, _ := json.Marshal(req)

	status, body, err := proxyRequest("POST", baseURL+"/register", jsonData, "")
	if err != nil {
		log.Println("REGISTER ERROR:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Println("REGISTER RAW RESPONSE:", string(body))

	// Parse and return only the required fields
	var regResp RegisterResponse
	if err := json.Unmarshal(body, &regResp); err == nil {
		c.JSON(status, regResp)
	} else {
		c.Data(status, "application/json", body)
	}
}


func authHandler(c *gin.Context) {
	var req AuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	jsonData, _ := json.Marshal(req)

	status, body, err := proxyRequest("POST", baseURL+"/auth", jsonData, "")
	if err != nil {
		log.Println("AUTH ERROR:", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Println("AUTH RAW RESPONSE:", string(body))

	// Parse and return only the required fields
	var authResp AuthResponse
	if err := json.Unmarshal(body, &authResp); err == nil && authResp.AccessToken != "" {
		setToken(authResp.AccessToken)
		log.Println("TOKEN SAVED:", authResp.AccessToken[:10]+"...")
		c.JSON(status, authResp)
	} else {
		c.Data(status, "application/json", body)
	}
}


func sendLog(stack, level, pkg, message string) {
	tok := getToken()
	if tok == "" {
		log.Println("NO TOKEN. SKIPPING LOG")
		return
	}

	payload := LogPayload{
		Stack:   stack,
		Level:   level,
		Package: pkg,
		Message: message,
	}
	jsonData, _ := json.Marshal(payload)

	status, body, err := proxyRequest("POST", baseURL+"/logs", jsonData, tok)
	if err != nil {
		log.Println("LOG ERROR:", err)
		return
	}

	log.Printf("LOG RESPONSE [%d]: %s", status, string(body))
}