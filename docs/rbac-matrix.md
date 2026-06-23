# GreenRoot RBAC Matrix

This matrix documents the intended V1 route authorization model before Admin UI work.

## Roles

| Role | Purpose |
| --- | --- |
| `ADMIN` | Platform operations, catalog management, audit visibility, global dashboard |
| `BUYER` | End user/customer flows, orders, subscriptions, own notifications |
| `NURSERY_OWNER` | Nursery profile, inventory, plant requests, order fulfillment participation |
| `DRIVER` | Dispatch assignment, driver profile, location/tracking updates |

## Route Policy

| Area | Public | Authenticated | Admin | Nursery Owner | Driver | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| Health | yes | no | no | no | no | `/health`, `/healthz`, `/readyz` |
| Swagger/OpenAPI | yes | no | no | no | no | Local/dev API contract |
| Auth | partial | yes | no | no | no | OTP public, `me/logout` protected |
| Users | no | own data | global read where allowed | own/member data | own data | Profile and session visibility must stay scoped |
| Plants | read | no | write | no | no | Public catalog read, admin catalog mutation |
| Nurseries | read | member actions | write/global | own nursery | no | Ownership/membership checks in service layer |
| Inventory | no | scoped read | global write | own nursery write | no | Nursery access must be checked before mutation |
| Plant Requests | no | scoped read | global | nursery workflows | no | Request/response flows are nursery-centered |
| Orders | no | own/scoped | global | nursery orders | no | Buyer and nursery access both valid |
| Payments | no | own/scoped | global | order-linked | no | Manual order payments now, gateway subscription payments later |
| Subscriptions | plans public | own | global/status | no | no | Plans public, subscription records protected |
| Vehicles | no | no | global write | no | no | Driver assignment depends on dispatch workflows |
| Drivers | no | own/scoped | global write | no | own profile/location | Driver route hardening should prefer own-driver checks |
| Dispatches | no | scoped | global | nursery dispatches | assigned | Tracking and status should remain scoped |
| Tracking | no | scoped | global | nursery dispatches | assigned | Latest/history reads must respect dispatch/driver ownership |
| Notifications | no | own | global/templates | own | own | Templates admin-only |
| Attachments | no | scoped | global | scoped | scoped | Entity-level access rules should be expanded as modules mature |
| Audit | no | no | yes | no | no | Admin-only |
| Admin | no | no | yes | no | no | Admin dashboard only |

## Hardening Checklist

* Add table-driven tests for every service role check.
* Add one negative test per protected route for missing JWT.
* Add one negative test per admin-only route for non-admin JWT.
* Keep authorization in service layer; handlers should only extract actor context.
