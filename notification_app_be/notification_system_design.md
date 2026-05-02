# notification_system_design.md

## Overview

The Notification System is a Go + Gin based backend service responsible for:

* Authenticating with the Evaluation API
* Receiving optimized maintenance plans from the Planner Service
* Generating depot maintenance notifications
* Storing notifications in memory
* Providing APIs to retrieve and clear notifications
* Logging all operations using the Evaluation Logging API

The system integrates with:

* Planner Service (`/plan`)
* Evaluation Authentication API
* Evaluation Logging API

Architecture and implementation are available in the uploaded Go services.  

# System Architecture

text

  Client/Postman  

       |
       v

 Notification Service  
 (Gin + Go)               
   |         |         |
   |         |         |
   v         v         v
 Auth API   Log API   Planner Service
                        |
                        v
                Vehicle Scheduler


# Core Components

## 1. Authentication Module

Responsible for:

* Authenticating users
* Obtaining bearer tokens
* Storing token safely using mutex locks

### Endpoint

http
POST /auth


### Request

json
{
  "email": "user@abc.com",
  "name": "John",
  "rollNo": "aa1bb",
  "accessCode": "xgAsNC",
  "clientID": "client-id",
  "clientSecret": "client-secret"
}


### Response

json
{
  "token_type": "Bearer",
  "access_token": "xxxxx",
  "expires_in": 3600
}


Authentication implementation uses thread-safe token storage. 



# 2. Logging Middleware

The middleware:

* Captures request body
* Captures response body
* Measures execution time
* Sends logs asynchronously to the Evaluation Log API

### Logged Information

* Request Method
* URL Path
* Request Body
* Response Body
* HTTP Status
* Execution Time

Middleware implementation: 

# 3. Planner Service Integration

The notification system calls:

http
GET /plan


from the planner backend.

Planner backend:

* Fetches depots
* Fetches vehicles
* Runs Knapsack Optimization
* Returns best maintenance allocation

Planner implementation: 

# 4. Notification Generator

The generator converts depot optimization results into readable notifications.

### Example Notification

json
{
  "id": 1,
  "depotId": "D1",
  "message": "Depot D1: Schedule 3 vehicles. Total hours: 9/10. Impact score: 22",
  "maxImpactScore": 22,
  "totalHoursUsed": 9,
  "mechanicHoursBudget": 10,
  "createdAt": "2026-05-02T10:00:00Z"
}


Notification generation implementation: 



# API Endpoints

# 1. Authenticate

http
POST /auth

Authenticates and stores bearer token.

# 2. Generate Notifications

http
POST /notifications/generate

### Flow

1. Validate token
2. Call planner service
3. Parse optimization results
4. Generate notifications
5. Store notifications
6. Send logs

### Response

json
{
  "generated": 2,
  "notifications": [
    {
      "id": 1,
      "depotId": "D1",
      "message": "Depot D1: Schedule 3 vehicles...",
      "score": 22
    }
  ]
}

# 3. Get Notifications

http
GET /notifications


Returns all generated notifications.


# 4. Clear Notifications

http
DELETE /notifications


Removes all notifications from memory.


# Knapsack Optimization

The planner service uses the 0/1 Knapsack Algorithm.

## Objective

Maximize:

text
Total Operational Impact Score


Subject to:

text
Total Vehicle Service Duration <= Mechanic Hours Budget


Algorithm implementation: 



# Data Structures

## Depot

go
type Depot struct {
	ID            int
	MechanicHours int
}


## Vehicle

go
type Vehicle struct {
	TaskID   string
	Duration int
	Impact   int
}


## Notification

go
type Notification struct {
	ID        int
	DepotID   string
	Message   string
	Score     int
	Hours     int
	Budget    int
	CreatedAt string
}


# Concurrency Handling

Mutex locks are used for:

* Bearer token storage
* Notification storage

This prevents:

* Race conditions
* Concurrent write corruption

Implementation uses:

```go
sync.RWMutex
```

Used in:  


# Error Handling

The system handles:

* Invalid JSON
* API failures
* Authentication failures
* Timeout errors
* Planner unavailability
* Token missing errors

Example:

json
{
  "error": "not authenticated, call /auth first"
}


# Security

## Token-based Authentication

All log API calls require:

http
Authorization: Bearer <token>


## TLS Configuration

Custom TLS transport handles:

* Self-signed certificates
* Evaluation environment SSL issues


# Performance

## Asynchronous Logging

Logs are sent using goroutines:

```go
go sendLog(...)
```

This avoids blocking request processing.

## Optimized HTTP Client

Shared HTTP client:

* Reduces connection overhead
* Uses timeout protection



# Technologies Used

 Technology          Purpose               

 Go                  Backend Language      
 Gin                 HTTP Framework        
 Mutex               Concurrency Control   
 REST APIs           Service Communication 
 JSON                Data Exchange         
 Knapsack Algorithm  Optimization          
 Goroutines          Async Processing      


# Conclusion

The Notification System provides:

* Secure authentication
* Centralized logging
* Maintenance optimization integration
* Real-time notification generation
* Concurrent-safe in-memory storage

The system is lightweight, modular, scalable, and suitable for evaluation environments and distributed backend architectures.
