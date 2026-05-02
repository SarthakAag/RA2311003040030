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

const (
	depotAPI   = "http://20.207.122.201/evaluation-service/depots"
	vehicleAPI = "http://20.207.122.201/evaluation-service/vehicles"
)



type Depot struct {
	ID            int `json:"ID"`
	MechanicHours int `json:"MechanicHours"`
}

type DepotsResponse struct {
	Depots []Depot `json:"depots"`
}

type Vehicle struct {
	TaskID   string `json:"TaskID"`
	Duration int    `json:"Duration"`
	Impact   int    `json:"Impact"`
}

type VehiclesResponse struct {
	Vehicles []Vehicle `json:"vehicles"`
}

type SelectedVehicle struct {
	TaskID   string `json:"TaskID"`
	Duration int    `json:"Duration"`
	Impact   int    `json:"Impact"`
}

type DepotResult struct {
	ID               int               `json:"ID"`
	MechanicHours    int               `json:"MechanicHours"`
	TotalHoursUsed   int               `json:"totalHoursUsed"`
	MaxImpactScore   int               `json:"maxImpactScore"`
	SelectedVehicles []SelectedVehicle `json:"selectedVehicles"`
}

type PlanResponse struct {
	Depots []DepotResult `json:"depots"`
}



var httpClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}



func knapsack(vehicles []Vehicle, budget int) (int, []SelectedVehicle) {
	n := len(vehicles)
	if n == 0 || budget <= 0 {
		return 0, []SelectedVehicle{}
	}

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, budget+1)
	}

	for i := 1; i <= n; i++ {
		v := vehicles[i-1]
		dur := v.Duration
		impact := v.Impact
		for w := 0; w <= budget; w++ {
			dp[i][w] = dp[i-1][w]
			if dur <= w && dp[i-1][w-dur]+impact > dp[i][w] {
				dp[i][w] = dp[i-1][w-dur] + impact
			}
		}
	}

	selected := []SelectedVehicle{}
	w := budget
	for i := n; i > 0; i-- {
		if dp[i][w] != dp[i-1][w] {
			v := vehicles[i-1]
			selected = append(selected, SelectedVehicle{
				TaskID:   v.TaskID,
				Duration: v.Duration,
				Impact:   v.Impact,
			})
			w -= v.Duration
		}
	}

	return dp[n][budget], selected
}


func depotsHandler(c *gin.Context) {
	token := c.GetHeader("Authorization")

	req, err := http.NewRequest("GET", depotAPI, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Println("DEPOTS RESPONSE:", string(body))

	var depotsResp DepotsResponse
	if err := json.Unmarshal(body, &depotsResp); err != nil {
		c.JSON(500, gin.H{"error": "parse error: " + err.Error()})
		return
	}

	c.JSON(200, DepotsResponse{Depots: depotsResp.Depots})
}

// GET /vehicles — returns TaskID, Duration, Impact
func vehiclesHandler(c *gin.Context) {
	token := c.GetHeader("Authorization")

	req, err := http.NewRequest("GET", vehicleAPI, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	log.Println("VEHICLES RESPONSE:", string(body))

	var vehiclesResp VehiclesResponse
	if err := json.Unmarshal(body, &vehiclesResp); err != nil {
		c.JSON(500, gin.H{"error": "parse error: " + err.Error()})
		return
	}

	c.JSON(200, vehiclesResp)
}

// GET /plan — runs knapsack using vehicles + depots
func planHandler(c *gin.Context) {
	token := c.GetHeader("Authorization")

	// Fetch depots
	req1, _ := http.NewRequest("GET", depotAPI, nil)
	if token != "" {
		req1.Header.Set("Authorization", token)
	}
	resp1, err := httpClient.Do(req1)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to fetch depots: " + err.Error()})
		return
	}
	defer resp1.Body.Close()
	body1, _ := io.ReadAll(resp1.Body)

	var depotsResp DepotsResponse
	json.Unmarshal(body1, &depotsResp)

	// Fetch vehicles
	req2, _ := http.NewRequest("GET", vehicleAPI, nil)
	if token != "" {
		req2.Header.Set("Authorization", token)
	}
	resp2, err := httpClient.Do(req2)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to fetch vehicles: " + err.Error()})
		return
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)

	var vehiclesResp VehiclesResponse
	json.Unmarshal(body2, &vehiclesResp)

	log.Println("DEPOTS:", string(body1))
	log.Println("VEHICLES:", string(body2))

	// Run knapsack for each depot using same vehicle list
	results := []DepotResult{}
	for _, depot := range depotsResp.Depots {
		maxScore, selected := knapsack(vehiclesResp.Vehicles, depot.MechanicHours)

		totalHours := 0
		for _, v := range selected {
			totalHours += v.Duration
		}

		results = append(results, DepotResult{
			ID:               depot.ID,
			MechanicHours:    depot.MechanicHours,
			TotalHoursUsed:   totalHours,
			MaxImpactScore:   maxScore,
			SelectedVehicles: selected,
		})
	}

	c.JSON(200, PlanResponse{Depots: results})
}

// ---------------- MAIN ----------------

func main() {
	r := gin.Default()

	r.GET("/depots", depotsHandler)
	r.GET("/vehicles", vehiclesHandler)
	r.GET("/plan", planHandler)

	log.Println("🚀 Vehicle Maintenance Scheduler running at :8081")
	r.Run(":8081")
}