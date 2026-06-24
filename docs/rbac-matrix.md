# GreenRoot RBAC Matrix

## Roles

| Role | Purpose |
| --- | --- |
| `ADMIN` | Platform operations, catalog management, audit visibility, global dashboard |
| `SUPER_ADMIN` | Full platform access including admin management |
| `NURSERY_OWNER` | Nursery profile, inventory, plant requests, order management |
| `MANAGER` | Works under a nursery — creates orders for buyers, manages inventory and dispatches |
| `DRIVER` | Dispatch assignment, driver profile, location/tracking updates |
| `TRANSPORT_PROVIDER` | Fleet/vehicle management for transport companies |
| `BUYER` | Read own orders and track dispatches (orders placed by nursery staff on their behalf) |

## Order Creation Flow

Orders are always created by nursery staff (`NURSERY_OWNER` or `MANAGER`), never by buyers directly. Staff call `POST /orders` with `buyer_mobile` + `buyer_name`. The API auto-creates a `BUYER` user if the mobile is not registered. Buyers log in to view their orders and track delivery (read-only).

## Route Policy

| Area | Public | Admin | Nursery Owner | Manager | Driver | Buyer | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Health | yes | — | — | — | — | — | `/healthz`, `/readyz` |
| Swagger | yes | — | — | — | — | — | Dev only |
| Auth | partial | — | — | — | — | — | OTP public, `me/logout` protected |
| Users | no | global | own/member | own | own | own | Profile scoped to self |
| Plants | read | write | read | read | read | read | Public catalog; admin mutates |
| Nurseries | read | write/global | own nursery | own nursery | no | read | Membership checked in service |
| Inventory | no | global | own nursery | own nursery | no | no | Nursery-scoped |
| Plant Requests | no | global | nursery B2B | nursery B2B | no | no | Nursery-to-nursery requests only |
| Orders | no | global | own nursery | own nursery | no | own orders | Manager creates; buyer reads |
| Payments | no | global | order-linked | no | no | own | Subscription fee only from platform |
| Subscriptions | plans public | global | own | no | no | no | Plans public |
| Vehicles | no | global write | no | no | assigned | no | |
| Drivers | no | global write | no | no | own profile | no | Location updates driver-only |
| Dispatches | no | global | own nursery | own nursery | assigned | own orders | Scoped to nursery/driver |
| Tracking | no | global | own nursery | own nursery | assigned | own orders | Reads respect ownership |
| Notifications | no | global/templates | own | own | own | own | Templates admin-only |
| Audit | no | yes | no | no | no | no | Admin-only |
| Admin | no | yes | no | no | no | no | Admin dashboard only |

## Hardening Checklist

- Add table-driven tests for every service role check.
- Add one negative test per protected route for missing JWT.
- Add one negative test per admin-only route for non-admin JWT.
- Keep authorization in service layer; handlers should only extract actor context.
