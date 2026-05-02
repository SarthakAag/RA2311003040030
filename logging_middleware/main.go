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


// if endpoint changes later maybe move to env.
const baseApi = "http://20.207.122.201/evaluation-service
"

var (
authToken string
locker sync.RWMutex
)

// tiny helper because I got tired of repeating lock code
func currentToken() string {
locker.RLock()
defer locker.RUnlock()

return authToken

}

func saveToken(t string) {
locker.Lock()
authToken = t
locker.Unlock()
}

// ---------------- REQUEST / RESPONSE MODELS ----------------

type RegisterReq struct {
Email string json:"email"
Name string json:"name"
Phone string json:"mobileNo"
GithubID string json:"githubUsername"

RollNo string `json:"rollNo"`
Code   string `json:"accessCode"`

}

type RegisterResp struct {
Email string json:"email"
Name string json:"name"

RollNo string `json:"rollNo"`

AccessCode string `json:"accessCode"`
ClientID   string `json:"clientID"`

ClientSecret string `json:"clientSecret"`

}

type LoginReq struct {
Email string json:"email"
Name string json:"name"

RollNo string `json:"rollNo"`

AccessCode string `json:"accessCode"`

ClientID     string `json:"clientID"`
ClientSecret string `json:"clientSecret"`

}

type LoginResp struct {
TokenType string json:"token_type"
Token string json:"access_token"

ExpiresIn int `json:"expires_in"`

}

type LogReq struct {
Stack string json:"stack"
Level string json:"level"

Package string `json:"package"`
Message string `json:"message"`

}

// ------------------------------------------------------------

func main() {

// gin default is enough for now
router := gin.Default()

router.Use(RequestLogger())

router.POST("/register", registerUser)
router.POST("/auth", doAuth)

log.Println("server booted on :8080")
router.Run(":8080")

}

var client = &http.Client{
Timeout: 20 * time.Second,

Transport: &http.Transport{
	TLSClientConfig: &tls.Config{
		// NOTE:
		// using this because SSL cert was acting weird during testing
		// TODO maybe remove later
		InsecureSkipVerify: true,
	},
},

}

// ------------------------------------------------------------

func RequestLogger() gin.HandlerFunc {

return func(ctx *gin.Context) {

	start := time.Now()

	bodyStuff, _ := io.ReadAll(ctx.Request.Body)
	ctx.Request.Body = io.NopCloser(bytes.NewBuffer(bodyStuff))

	log.Println("----------")
	log.Printf("%s -> %s", ctx.Request.Method, ctx.Request.URL.Path)

	if len(bodyStuff) > 0 {
		log.Println("payload:", string(bodyStuff))
	}

	// wrapping writer so we can see response too
	writer := &responseSaver{
		ResponseWriter: ctx.Writer,
		body:           bytes.NewBufferString(""),
	}

	ctx.Writer = writer

	ctx.Next()

	took := time.Since(start)

	log.Printf("status=%d took=%s", ctx.Writer.Status(), took)
	log.Println("response:", writer.body.String())

	token := currentToken()

	if token == "" {
		log.Println("skip log push because token missing")
		return
	}

	// async because no point blocking request
	go sendLogs(
		"backend",
		"info",
		"middleware",
		"request completed in "+took.String(),
	)
}

}

// custom response writer thing
// I copied this pattern from an old project tbh
type responseSaver struct {
gin.ResponseWriter
body *bytes.Buffer
}

func (w *responseSaver) Write(data []byte) (int, error) {
w.body.Write(data)
return w.ResponseWriter.Write(data)
}

// ------------------------------------------------------------

func hitApi(method string, url string, payload []byte, token string) (int, []byte, error) {

req, err := http.NewRequest(method, url, bytes.NewBuffer(payload))
if err != nil {
	return 0, nil, err
}

req.Header.Set("Content-Type", "application/json")

if token != "" {
	req.Header.Set("Authorization", "Bearer "+token)
}

resp, err := client.Do(req)
if err != nil {
	return 0, nil, err
}

defer resp.Body.Close()

respBody, _ := io.ReadAll(resp.Body)

return resp.StatusCode, respBody, nil

}

// ------------------------------------------------------------

func registerUser(ctx *gin.Context) {

var data RegisterReq

if err := ctx.ShouldBindJSON(&data); err != nil {

	ctx.JSON(400, gin.H{
		"error": err.Error(),
	})

	return
}

body, _ := json.Marshal(data)

status, res, err := hitApi(
	"POST",
	baseApi+"/register",
	body,
	"",
)

if err != nil {

	log.Println("register failed:", err)

	ctx.JSON(500, gin.H{
		"error": "something went wrong",
	})

	return
}

log.Println("register api response:", string(res))

var parsed RegisterResp

// not failing hard here intentionally
if json.Unmarshal(res, &parsed) == nil {

	ctx.JSON(status, parsed)

} else {

	// fallback if response shape changes
	ctx.Data(status, "application/json", res)
}

}

// ------------------------------------------------------------

func doAuth(ctx *gin.Context) {

var creds LoginReq

if err := ctx.ShouldBindJSON(&creds); err != nil {

	ctx.JSON(400, gin.H{
		"error": err.Error(),
	})

	return
}

body, _ := json.Marshal(creds)

status, response, err := hitApi(
	"POST",
	baseApi+"/auth",
	body,
	"",
)

if err != nil {

	log.Println("auth issue:", err)

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

log.Println("auth raw:", string(response))

var auth LoginResp

if json.Unmarshal(response, &auth) == nil && auth.Token != "" {

	saveToken(auth.Token)

	// just showing partial token because paranoia lol
	log.Println("saved token:", auth.Token[:10]+"...")

	ctx.JSON(status, auth)

} else {

	ctx.Data(status, "application/json", response)
}

}

// ------------------------------------------------------------

func sendLogs(stack, level, pkg, msg string) {

token := currentToken()

if token == "" {
	log.Println("cannot send logs, token empty")
	return
}

reqBody := LogReq{
	Stack:   stack,
	Level:   level,
	Package: pkg,
	Message: msg,
}

jsonBody, _ := json.Marshal(reqBody)

status, body, err := hitApi(
	"POST",
	baseApi+"/logs",
	jsonBody,
	token,
)

if err != nil {
	log.Println("log api failed:", err)
	return
}

log.Printf("log pushed [%d]", status)

// sometimes useful during debugging
if len(body) > 0 {
	log.Println(string(body))
}

}
