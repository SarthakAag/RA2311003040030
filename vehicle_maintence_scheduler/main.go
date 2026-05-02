package main

import (
"crypto/tls"
"encoding/json"
"io"
"log"
"net/http"
"time"

"github.com/gin-gonic/gin"

)

// probably should move to env vars eventually
const (
depotUrl = "http://20.207.122.201/evaluation-service/depots
"
vehicleUrl = "http://20.207.122.201/evaluation-service/vehicles
"
)

// -----------------------------------------------------
// models
// -----------------------------------------------------

type Depot struct {
ID int json:"ID"

MechanicHours int `json:"MechanicHours"`

}

type DepotApiResponse struct {
Depots []Depot json:"depots"
}

type Vehicle struct {
TaskID string json:"TaskID"

Duration int `json:"Duration"`

Impact int `json:"Impact"`

}

type VehicleApiResponse struct {
Vehicles []Vehicle json:"vehicles"
}

type SelectedVehicle struct {
TaskID string json:"TaskID"

Duration int `json:"Duration"`

Impact int `json:"Impact"`

}

type DepotPlan struct {
ID int json:"ID"

MechanicHours int `json:"MechanicHours"`

TotalHours int `json:"totalHoursUsed"`

MaxScore int `json:"maxImpactScore"`

SelectedVehicles []SelectedVehicle `json:"selectedVehicles"`

}

type FinalPlan struct {
Depots []DepotPlan json:"depots"
}

// -----------------------------------------------------
// http client
// -----------------------------------------------------

var client = &http.Client{
Timeout: 20 * time.Second,

Transport: &http.Transport{
	TLSClientConfig: &tls.Config{

		// ssl issue happened once locally
		// leaving this for now
		InsecureSkipVerify: true,
	},
},

}

// -----------------------------------------------------
// knapsack algo
// -----------------------------------------------------

func runKnapsack(
vehicles []Vehicle,
hours int,
) (int, []SelectedVehicle) {

totalVehicles := len(vehicles)

if totalVehicles == 0 || hours <= 0 {

	return 0, []SelectedVehicle{}
}

// dp table
dp := make([][]int, totalVehicles+1)

for i := range dp {

	dp[i] = make([]int, hours+1)
}

for i := 1; i <= totalVehicles; i++ {

	currentVehicle := vehicles[i-1]

	duration := currentVehicle.Duration
	impact := currentVehicle.Impact

	for currentHours := 0; currentHours <= hours; currentHours++ {

		// skip current vehicle
		dp[i][currentHours] = dp[i-1][currentHours]

		// include current vehicle
		if duration <= currentHours {

			newImpact :=
				dp[i-1][currentHours-duration] + impact

			if newImpact > dp[i][currentHours] {

				dp[i][currentHours] = newImpact
			}
		}
	}
}

selected := []SelectedVehicle{}

remainingHours := hours

// walking backwards through dp table
for i := totalVehicles; i > 0; i-- {

	if dp[i][remainingHours] != dp[i-1][remainingHours] {

		item := vehicles[i-1]

		selected = append(
			selected,
			SelectedVehicle{
				TaskID:   item.TaskID,
				Duration: item.Duration,
				Impact:   item.Impact,
			},
		)

		remainingHours -= item.Duration
	}
}

return dp[totalVehicles][hours], selected

}

// -----------------------------------------------------
// depots endpoint
// -----------------------------------------------------

func depotsHandler(ctx *gin.Context) {

token := ctx.GetHeader("Authorization")

req, err := http.NewRequest(
	"GET",
	depotUrl,
	nil,
)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

if token != "" {

	req.Header.Set(
		"Authorization",
		token,
	)
}

resp, err := client.Do(req)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

defer resp.Body.Close()

body, _ := io.ReadAll(resp.Body)

log.Println("depots api:", string(body))

var depotData DepotApiResponse

if err := json.Unmarshal(body, &depotData); err != nil {

	ctx.JSON(500, gin.H{
		"error": "parse failed: " + err.Error(),
	})

	return
}

ctx.JSON(200, depotData)

}

// -----------------------------------------------------
// vehicles endpoint
// -----------------------------------------------------

func vehiclesHandler(ctx *gin.Context) {

token := ctx.GetHeader("Authorization")

req, err := http.NewRequest(
	"GET",
	vehicleUrl,
	nil,
)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

if token != "" {
	req.Header.Set("Authorization", token)
}

resp, err := client.Do(req)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": err.Error(),
	})

	return
}

defer resp.Body.Close()

body, _ := io.ReadAll(resp.Body)

log.Println("vehicles api:", string(body))

var vehicleData VehicleApiResponse

if err := json.Unmarshal(body, &vehicleData); err != nil {

	ctx.JSON(500, gin.H{
		"error": "vehicle parse failed",
	})

	return
}

ctx.JSON(200, vehicleData)

}

// -----------------------------------------------------
// planner endpoint
// -----------------------------------------------------

func planHandler(ctx *gin.Context) {

token := ctx.GetHeader("Authorization")

// fetching depots first
depotReq, _ := http.NewRequest(
	"GET",
	depotUrl,
	nil,
)

if token != "" {
	depotReq.Header.Set("Authorization", token)
}

depotResp, err := client.Do(depotReq)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": "could not fetch depots",
	})

	return
}

defer depotResp.Body.Close()

depotBody, _ := io.ReadAll(depotResp.Body)

var depots DepotApiResponse

json.Unmarshal(depotBody, &depots)

// now fetch vehicles
vehicleReq, _ := http.NewRequest(
	"GET",
	vehicleUrl,
	nil,
)

if token != "" {
	vehicleReq.Header.Set("Authorization", token)
}

vehicleResp, err := client.Do(vehicleReq)

if err != nil {

	ctx.JSON(500, gin.H{
		"error": "could not fetch vehicles",
	})

	return
}

defer vehicleResp.Body.Close()

vehicleBody, _ := io.ReadAll(vehicleResp.Body)

var vehicles VehicleApiResponse

json.Unmarshal(vehicleBody, &vehicles)

log.Println("depots raw:", string(depotBody))
log.Println("vehicles raw:", string(vehicleBody))

// same vehicle list reused for all depots
results := []DepotPlan{}

for _, depot := range depots.Depots {

	score, selectedVehicles :=
		runKnapsack(
			vehicles.Vehicles,
			depot.MechanicHours,
		)

	totalUsed := 0

	for _, item := range selectedVehicles {

		totalUsed += item.Duration
	}

	result := DepotPlan{
		ID:               depot.ID,
		MechanicHours:    depot.MechanicHours,
		TotalHours:       totalUsed,
		MaxScore:         score,
		SelectedVehicles: selectedVehicles,
	}

	results = append(results, result)
}

ctx.JSON(200, FinalPlan{
	Depots: results,
})

}

// -----------------------------------------------------
// main
// -----------------------------------------------------

func main() {

router := gin.Default()

router.GET("/depots", depotsHandler)

router.GET("/vehicles", vehiclesHandler)

router.GET("/plan", planHandler)

log.Println(
	"vehicle scheduler running on :8081",
)

router.Run(":8081")

}
