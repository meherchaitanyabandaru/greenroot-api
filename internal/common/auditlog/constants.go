package auditlog

// EntityType identifies the kind of record that was affected.
type EntityType = string

const (
	EntityOrder        EntityType = "order"
	EntityOrderItem    EntityType = "order_item"
	EntityPlant        EntityType = "plant"
	EntityVehicle      EntityType = "vehicle"
	EntityUser         EntityType = "user"
	EntityUserAddress  EntityType = "user_address"
	EntityNursery      EntityType = "nursery"
	EntityNurseryUser  EntityType = "nursery_user"
	EntityNurseryAddr  EntityType = "nursery_address"
	EntityQuotation    EntityType = "quotation"
	EntityPayment      EntityType = "payment"
	EntitySubscription EntityType = "subscription"
	EntityInvite       EntityType = "invite"
	EntityDispatch     EntityType = "dispatch"
	EntityDriver       EntityType = "driver"
	EntityRequest      EntityType = "request"
	EntityInventory    EntityType = "inventory"
	EntityNotification EntityType = "notification"
	EntitySourcing     EntityType = "sourcing_post"
	EntityMarketAd     EntityType = "market_ad"
)

// Module identifies the business domain that produced the event.
type Module = string

const (
	ModuleAuth          Module = "AUTH"
	ModuleUsers         Module = "USERS"
	ModuleNurseries     Module = "NURSERIES"
	ModuleOrders        Module = "ORDERS"
	ModuleDispatches    Module = "DISPATCHES"
	ModuleQuotations    Module = "QUOTATIONS"
	ModuleLocalMarket   Module = "LOCAL_MARKET"
	ModuleRequests      Module = "REQUESTS"
	ModulePlants        Module = "PLANTS"
	ModuleInventory     Module = "INVENTORY"
	ModulePayments      Module = "PAYMENTS"
	ModuleSubscriptions Module = "SUBSCRIPTIONS"
	ModuleSourcing      Module = "SOURCING"
	ModuleDrivers       Module = "DRIVERS"
	ModuleVehicles      Module = "VEHICLES"
	ModuleInvites       Module = "INVITES"
	ModuleNotifications Module = "NOTIFICATIONS"
)

// Action is the operation performed on an entity.
type Action = string

const (
	ActionCreate   Action = "CREATE"
	ActionUpdate   Action = "UPDATE"
	ActionDelete   Action = "DELETE"
	ActionAssign   Action = "ASSIGN"
	ActionApprove  Action = "APPROVE"
	ActionReject   Action = "REJECT"
	ActionCancel   Action = "CANCEL"
	ActionComplete Action = "COMPLETE"
	ActionDispatch Action = "DISPATCH"
	ActionDeliver  Action = "DELIVER"
	ActionSuspend  Action = "SUSPEND"
	ActionActivate Action = "ACTIVATE"
	ActionLogin    Action = "LOGIN"
	ActionLogout   Action = "LOGOUT"
	ActionAccept   Action = "ACCEPT"
	ActionRecall   Action = "RECALL"
	ActionDownload Action = "DOWNLOAD"
	ActionUpload   Action = "UPLOAD"
	ActionRenew    Action = "RENEW"
	ActionBlock    Action = "BLOCK"
	ActionUnblock  Action = "UNBLOCK"
)

// SecurityEvent classifies security-sensitive audit events.
type SecurityEvent = string

const (
	SecurityEventLogin               SecurityEvent = "LOGIN"
	SecurityEventLogout              SecurityEvent = "LOGOUT"
	SecurityEventLoginFailed         SecurityEvent = "LOGIN_FAILED"
	SecurityEventTokenRevoked        SecurityEvent = "TOKEN_REVOKED"
	SecurityEventPermissionDenied    SecurityEvent = "PERMISSION_DENIED"
	SecurityEventAccountSuspended    SecurityEvent = "ACCOUNT_SUSPENDED"
	SecurityEventNurserySuspended    SecurityEvent = "NURSERY_SUSPENDED"
	SecurityEventSubscriptionBlocked SecurityEvent = "SUBSCRIPTION_BLOCKED"
	SecurityEventJWTFailure          SecurityEvent = "JWT_VALIDATION_FAILURE"
	SecurityEventAdminOverride       SecurityEvent = "ADMIN_OVERRIDE"
)
