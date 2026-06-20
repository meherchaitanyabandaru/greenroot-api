# GreenRoot API

GreenRoot API is the core backend platform responsible for authentication, dispatch management, trip tracking, GPS updates, delivery verification, notifications, and audit logging.

---

## Overview

The API serves:

* GreenRoot Mobile App
* GreenRoot Admin Portal
* Future Marketplace Services
* Future Analytics Platform

---

## Responsibilities

### Authentication

* Mobile OTP Login
* JWT Tokens
* Refresh Tokens
* Device Registration

---

### User Management

* Owners
* Gumastas
* Drivers
* Administrators

---

### Nursery Management

* Nursery Registration
* Nursery Approval
* Nursery Profile
* Subscription Status

---

### Dispatch Management

* Create Dispatch
* Update Dispatch
* Cancel Dispatch
* Approve Dispatch

---

### Plant Manifest

Manage:

* Plant Name
* Plant Category
* Plant Size
* Quantity

---

### Driver Management

* Driver Registration
* Driver Assignment
* Driver Availability
* Driver History

---

### Trip Management

* Create Trip
* Start Trip
* Complete Trip
* Partial Delivery
* Trip Timeline

---

### GPS Tracking

* Driver Location Updates
* Active Vehicle Tracking
* Trip Monitoring

---

### Photo Management

* Loading Photos
* Delivery Photos
* Proof Of Delivery

Stored in AWS S3.

---

### Notifications

* Push Notifications
* System Alerts
* Dispatch Updates

---

### Audit Logging

Track:

* Created By
* Updated By
* Approved By
* Delivered By

---

## Technology Stack

### Language

Go

### Framework

Gin

### Database

PostgreSQL

### Storage

AWS S3

### Authentication

Firebase Auth

### Notifications

Firebase Cloud Messaging

### Monitoring

AWS CloudWatch

---

## Architecture

```text
Mobile App
      ↓
GreenRoot API
      ↓
PostgreSQL
      ↓
AWS S3
```

---

## Project Structure

```text
cmd/

internal/

├── auth/
├── users/
├── nurseries/
├── dispatches/
├── plants/
├── drivers/
├── trips/
├── tracking/
├── photos/
├── notifications/
├── subscriptions/
├── audit/

pkg/

configs/

migrations/

docs/
```

---

## API Modules

### Auth Module

Responsibilities:

* Login
* Logout
* OTP Verification
* Token Management

---

### User Module

Responsibilities:

* User CRUD
* Role Management
* Device Management

---

### Nursery Module

Responsibilities:

* Nursery Registration
* Nursery Profile
* Subscription Validation

---

### Dispatch Module

Responsibilities:

* Create Dispatch
* Update Dispatch
* Approval Workflow

---

### Tracking Module

Responsibilities:

* GPS Updates
* Vehicle Location
* Trip Tracking

---

### Audit Module

Responsibilities:

* Action History
* Compliance Logs
* Activity Tracking

---

## Database

Primary Database:

```text
PostgreSQL
```

Core Tables:

```text
users
nurseries
drivers
vehicles
dispatches
dispatch_items
trips
trip_locations
photos
notifications
subscriptions
audit_logs
```

---

## Environments

### Development

```text
DEV
```

### Production

```text
PROD
```

---

## Security

* JWT Authentication
* OTP Verification
* Device Validation
* API Rate Limiting
* Audit Logs
* HTTPS Only

---

## Monitoring

* CloudWatch Logs
* CloudWatch Metrics
* Health Checks
* Error Tracking

---

## Future Roadmap

### V1

* Dispatch Platform
* GPS Tracking
* Delivery Verification

### V2

* Nursery Network
* Customer Directory
* Business Insights

### V3

* Plant Marketplace
* Customer Ordering
* AI Recommendations
* Demand Forecasting

---

## Product Vision

GreenRoot API serves as the central platform powering plant dispatch, transportation visibility, and future nursery commerce across India.
