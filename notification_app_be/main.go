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

// might move these to env later.
// keeping hardcoded while testing locally.
const (
apiBase = "http://20.207.122.201/evaluation-service"
planApi = "http://localhost:8081/plan"
)

// ---------------------------------------------------
// globals
// ---------------------------------------------------

var (
tokenValue string
tokenLock sync.RWMutex

allNotifications []Notification
notifLock         sync.RWMutex

)

// ---------------------------------------------------
// token helpers
// ---------------------------------------------------

func fetchToken() string {

tokenLock.RLock()
defer tokenLock.RUnlock()

return tokenValue

}

func updateToken(t string) {

tokenLock.Lock()
tokenValue = t
tokenLock.Unlock()

}

// ---------------------------------------------------
// models
// ---------------------------------------------------

type AuthReq struct {
Email string json:"email"
Name string json:"name"

RollNo string `json:"rollNo"`

AccessCode string `json:"accessCode"`

ClientID     string `json:"clientID"`
ClientSecret string `json:"clientSecret"`

}

type AuthResp struct {
TokenType string json:"token_type"

Token string `json:"access_token"`

Expires int `json:"expires_in"`

}

type LogReq struct {
Stack string json:"stack"
Level string json:"level"

Package string `json:"package"`
Message string `json:"message"`

}

// final response format
type Notification struct {
ID string json:"ID"

Type string `json:"Type"`

Message string `json:"Message"`

Timestamp string `json:"Timestamp"`

}

// planner side structs

type Vehicle struct {
ID string json:"id"

ServiceDuration int `json:"serviceDurationInHours"`

ImpactScore int `json:"operationalImpactScore"`

}

type DepotData struct {
DepotID string json:"depotId"

MechanicHours int `json:"mechanicHoursBudget"`

TotalHours int `json:"totalHoursUsed"`

MaxScore int `json:"maxImpactScore"`

Selected []Vehicle `json:"selectedVehicles"`

}

type PlannerResp struct {
Results []DepotData json:"results"
}

// ---------------------------------------------------
// http client
// ---------------------------------------------------

var apiClient = &http.Client{
Timeout: 20 * time.Second,

Transport: &http.Transport{
	TLSClientConfig: &tls.Config{

		// NOTE:
		// cert issue happened once during local setup
		// revisit later maybe
		InsecureSkipVerify: true,
	},
},

}

// ---------------------------------------------------
// middleware stuff
// ---------------------------------------------------

type responseLogger struct {
gin.ResponseWriter
body *bytes.Buffer
}

func (w *responseLogger) Write(data []byte) (int, error) {

w.body.Write(data)

return w.ResponseWriter.Write(data)

}

func RequestLogger() gin.HandlerFunc {

return func(ctx *gin.Context) {

	start := time.Now()

	bodyBytes, _ := io.ReadAll(ctx.Request.Body)

	ctx.Request.Body = io.NopCloser(
		bytes.NewBuffer(bodyBytes),
	)

	log.Println("-------------")
	log.Printf(
		"%s %s",
		ctx.Request.Method,
		ctx.Request.URL.Path,
	)

	if len(bodyBytes) > 0 {
		log.Println("body:", string(bodyBytes))
	}

	writer := &responseLogger{
		body:           bytes.NewBufferString(""),
		ResponseWriter: ctx.Writer,
	}

	ctx.Writer = writer

	ctx.Next()

	took := time.Since(start)

	log.Printf("status=%d", ctx.Writer.Status())
	log.Printf("took=%s", took)

	log.Println("response:", writer.body.String())

	if tok := fetchToken(); tok != "" {

		// async because logging should not slow request
		go sendLogs(
			"backend",
			"info",
			"middleware",
			"request finished",
		)
	}
}

}

// ---------------------------------------------------
// generic api helper
// ---------------------------------------------------

func makeRequest(
method string,
url string,
payload []byte,
token string,
) (int, []byte, error) {

req, err := http.NewRequest(
	method,
	url,
	bytes.NewBuffer(payload),
)

if err != nil {
	return 0, nil, err
}

req.Header.Set(
	"Content-Type",
	"application/json",
)

if token != "" {

	req.Header.Set(
		"Authorization",
		"Bearer "+token,
	)
}

resp, err := apiClient.Do(req)

if err != nil {
	return 0, nil, err
}

defer resp.Body.Close()

respBody, _ := io.ReadAll(resp.Body)

return resp.StatusCode, respBody, nil

}

// random helper
func newID() string {
return uuid.New().String()
}

// ---------------------------------------------------
// auth
// ---------------------------------------------------

func authHandler(ctx *gin.Context) {

var req AuthReq

if err := ctx.ShouldBindJSON(&req); err != nil {

	ctx.JSON(400, gin.H{
		"error": err.Error(),
	})

	return
}

jsonBody, _ := json.Marshal(req)

status, response, err := makeRequest(
	"POST",
	apiBase+"/auth",
	jsonBody,
	"",
)

if err != nil {

	log.Println("auth failed:", err)

	ctx.JSON(500, gin.H{
		"error": "auth failed",
	})

	return
}

var authData AuthResp

if json.Unmarshal(response, &authData) == nil &&
	authData.Token != "" {

	updateToken(authData.Token)

	log.Println("token stored")

	ctx.JSON(status, authData)

} else {

	// fallback if external api changes shape
	ctx.Data(status, "application/json", response)
}

}

// ---------------------------------------------------
// generate notifications
// ---------------------------------------------------

func generateNotificationsHandler(ctx *gin.Context) {

token := fetchToken()

if token == "" {

	ctx.JSON(401, gin.H{
		"error": "authenticate first",
	})

	return
}

req, err := http.NewRequest(
	"GET",
	planApi,
	nil,
)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

req.Header.Set(
	"Authorization",
	"Bearer "+token,
)

resp, err := apiClient.Do(req)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

defer resp.Body.Close()

body, _ := io.ReadAll(resp.Body)

var planner PlannerResp

if err := json.Unmarshal(body, &planner); err != nil {

	log.Println("planner parse issue:", err)

	ctx.JSON(500, gin.H{
		"error": "bad planner response",
	})

	return
}

notifLock.Lock()

tempNotifications := []Notification{}

for _, depot := range planner.Results {

	item := Notification{
		ID:        newID(),
		Type:      "Result",
		Message:   buildMessage(depot),
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
	}

	allNotifications = append(allNotifications, item)
	tempNotifications = append(tempNotifications, item)
}

notifLock.Unlock()

go sendLogs(
	"backend",
	"info",
	"notifications",
	"notifications created",
)

ctx.JSON(200, gin.H{
	"notifications": tempNotifications,
})

}

// ---------------------------------------------------
// fetch notifications
// ---------------------------------------------------

func getNotificationsHandler(ctx *gin.Context) {

notifLock.RLock()
defer notifLock.RUnlock()

ctx.JSON(200, gin.H{
	"notifications": allNotifications,
})

}

// ---------------------------------------------------
// clear notifications
// ---------------------------------------------------

func clearNotificationsHandler(ctx *gin.Context) {

notifLock.Lock()

// easiest reset
allNotifications = []Notification{}

notifLock.Unlock()

ctx.JSON(200, gin.H{
	"message": "notifications cleared",
})

}

// ---------------------------------------------------
// formatter
// ---------------------------------------------------

func buildMessage(d DepotData) string {

// maybe improve wording later
return "Depot " + d.DepotID +
	" scheduled with impact score " +
	intToString(d.MaxScore)

}

// ---------------------------------------------------
// homemade int to string lol
// strconv.Itoa exists but keeping this for fun
// ---------------------------------------------------

func intToString(n int) string {

if n == 0 {
	return "0"
}

output := ""

for n > 0 {

	lastDigit := n % 10

	output = string(rune('0'+lastDigit)) + output

	n = n / 10
}

return output

}

// ---------------------------------------------------
// logging service
// ---------------------------------------------------

func sendLogs(
stack string,
level string,
pkg string,
msg string,
) {

token := fetchToken()

if token == "" {
	log.Println("token missing, skipping logs")
	return
}

payload := LogReq{
	Stack:   stack,
	Level:   level,
	Package: pkg,
	Message: msg,
}

jsonBody, _ := json.Marshal(payload)

status, body, err := makeRequest(
	"POST",
	apiBase+"/logs",
	jsonBody,
	token,
)

if err != nil {

	log.Println("failed sending logs:", err)

	return
}

log.Printf("log response [%d]", status)

if len(body) > 0 {
	log.Println(string(body))
}

}

// ---------------------------------------------------
// main
// ---------------------------------------------------

func main() {

router := gin.Default()

router.Use(RequestLogger())

// auth
router.POST("/auth", authHandler)

// notification routes
router.POST(
	"/notifications/generate",
	generateNotificationsHandler,
)

router.GET(
	"/notifications",
	getNotificationsHandler,
)

router.DELETE(
	"/notifications",
	clearNotificationsHandler,
)

log.Println("notification system running on :8080")

router.Run(":8080")

}
