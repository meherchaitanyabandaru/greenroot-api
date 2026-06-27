# 🔐 GreenRoot V1 — RBAC Reference

> **Source of truth:** `bussiness-rules.md` and `bussiness_rules2.md`
> Cross-checked against all `service.go` files and actual route registrations.

---

## 👥 Roles

| # | Role | Symbol | Who They Are |
|---|---|---|---|
| 1 | `SUPER_ADMIN` / `ADMIN` | 👑 | Platform team. Manages GreenRoot platform. Cannot participate in business transactions. |
| 2 | `NURSERY_OWNER` | 🌳 | Owns exactly one nursery. Full business control within it. |
| 3 | `MANAGER` | 👨‍💼 | Works under one nursery (exclusive). Operational access only. |
| 4 | `DRIVER` | 🚛 | Independent. Joins trips via UUID/QR. No nursery affiliation. |
| 5 | `CUSTOMER` / `BUYER` | 🤝 | Buys from nurseries. Sees only own data. |

---

---

## 1. 🔑 Auth & Session

### 📋 Business Rules
- Login is mobile number + OTP only. No passwords.
- One mobile number = one GreenRoot account.
- Every user has one profile.
- Sessions are tracked per device.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| Send OTP | ✅ | ✅ | ✅ | ✅ | ✅ |
| Verify OTP / Login | ✅ | ✅ | ✅ | ✅ | ✅ |
| Refresh token | ✅ | ✅ | ✅ | ✅ | ✅ |
| Logout | ✅ | ✅ | ✅ | ✅ | ✅ |
| View own workspaces | ✅ | ✅ | ✅ | ✅ | ✅ |
| View owner dashboard | ❌ | ✅ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Send OTP | `POST /api/v1/auth/send-otp` | `otp_requests`, `users` | — | Login Screen → "Send OTP" button |
| Verify OTP | `POST /api/v1/auth/verify-otp` | `otp_requests`, `user_sessions` | Login Page | OTP Screen → "Verify" button |
| Refresh token | `POST /api/v1/auth/refresh-token` | `user_sessions` | Silent (background) | Silent (background) |
| Logout | `POST /api/v1/auth/logout` | `user_sessions` | Top-right → "Logout" | Profile tab → "Logout" |
| View workspaces | `GET /api/v1/me/workspaces` | `nurseries`, `nursery_users`, `drivers` | — | Workspace Select Screen |
| Owner dashboard | `GET /api/v1/me/owner-dashboard` | `nurseries`, `orders`, `quotations` | — | Owner Dashboard Screen |

---

## 2. 🌿 Plants — Master Catalogue

### 📋 Business Rules
- Plants are **GreenRoot platform master data** — not nursery data.
- Admins create and maintain the plant catalogue.
- All other roles are read-only consumers.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List plants | ✅ | ✅ | ✅ | ❌ | ✅ |
| View plant details | ✅ | ✅ | ✅ | ❌ | ✅ |
| View plant care guide | ✅ | ✅ | ✅ | ❌ | ✅ |
| View plant sizes | ✅ | ✅ | ✅ | ❌ | ✅ |
| View plant names | ✅ | ✅ | ✅ | ❌ | ✅ |
| **Create** plant | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Update** plant | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Delete** plant | ✅ | ❌ | ❌ | ❌ | ❌ |
| Upload plant image | ✅ | ❌ | ❌ | ❌ | ❌ |
| Manage categories | ✅ | ❌ | ❌ | ❌ | ❌ |

> ❌ Plants belong to the GreenRoot platform — not to any nursery.
> ❌ Drivers have no reason to browse the catalogue — purely logistics role.

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List plants | `GET /api/v1/plants` | `plants`, `plant_names` | Plants page → data table | Plant List Screen |
| View plant | `GET /api/v1/plants/{id}` | `plants`, `plant_names`, `plant_images` | Row click → `PlantDetailPanel` | Plant Detail Screen |
| Plant sizes | `GET /api/v1/plants/sizes` | `plant_sizes` | — | Plant Detail Screen |
| Plant names | `GET /api/v1/plants/names` | `plant_names` | — | — |
| Create plant | `POST /api/v1/plants` | `plants`, `plant_names` | "Add Plant" → `PlantForm` | — |
| Update plant | `PUT /api/v1/plants/{id}` | `plants`, `plant_names` | `PlantDetailPanel` → "Edit" | — |
| Delete plant | `DELETE /api/v1/plants/{id}` | `plants` | `PlantDetailPanel` → "Delete" | — |
| Upload image | `POST /api/v1/plants/{id}/images` | `plant_images`, `attachments` | `PlantImageForm` → "Upload Image" | — |
| Care guide | `GET /api/v1/plants/{id}/care-guide` | `plant_care_guides` | `PlantDetailPanel` → Care tab | Plant Detail → Care tab |
| List categories | `GET /api/v1/plants/categories` | `plant_categories` | Category Management Page | — |
| Create category | `POST /api/v1/plants/categories` | `plant_categories` | `CategoryManagementPage` → "Add Category" | — |
| Update category | `PUT /api/v1/plants/categories/{categoryId}` | `plant_categories` | Category row → "Edit" | — |
| Delete category | `DELETE /api/v1/plants/categories/{categoryId}` | `plant_categories` | Category row → "Delete" | — |

---

## 3. 🏡 Nurseries

### 📋 Business Rules
- One owner → one nursery. Shared or multiple ownership is not supported.
- Nursery must be APPROVED by Admin before owner can create quotations, orders, or invites.
- Managers join via UUID/QR — they never own or modify the nursery profile.
- Customers can register a nursery and become an owner later.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List all nurseries | ✅ | ✅ | ✅ | ✅ | ✅ |
| View nursery profile | ✅ | ✅ | ✅ | ✅ | ✅ |
| Get nurseries I manage (`/nurseries/mine`) | ✅ | ❌ | ✅ | ❌ | ❌ |
| Get nursery I own (`/nurseries/owned`) | ✅ | ✅ | ❌ | ❌ | ❌ |
| **Register** nursery | ✅ | ✅ (one only) | ❌ | ❌ | ✅ (becomes owner) |
| **Update** nursery profile | ✅ | own only | ❌ | ❌ | ❌ |
| **Approve** / Reject / Suspend nursery | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Delete** nursery | ✅ | ❌ | ❌ | ❌ | ❌ |
| Manage nursery addresses | ✅ | own only | ❌ | ❌ | ❌ |
| View managers list | ✅ | own only | ❌ | ❌ | ❌ |
| Add / Remove manager | ✅ | own only | ❌ | ❌ | ❌ |
| Connect driver to nursery | ✅ | ✅ | ✅ | ❌ | ❌ |
| Approve driver connection | ✅ | ✅ (own) | ❌ | ❌ | ❌ |
| List connected drivers | ✅ | own + member | own + member | ❌ | ❌ |
| View nursery applications | ✅ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List nurseries | `GET /api/v1/nurseries` | `nurseries` | Nurseries page → data table | Nursery List Screen |
| View nursery | `GET /api/v1/nurseries/{id}` | `nurseries`, `nursery_addresses` | Row click → `NurseryDetailPanel` | Nursery Detail Screen |
| My managed nurseries | `GET /api/v1/nurseries/mine` | `nursery_users`, `nurseries` | — | Workspace Select Screen |
| My owned nursery | `GET /api/v1/nurseries/owned` | `nurseries` | — | Owner Dashboard |
| Register nursery | `POST /api/v1/nurseries` | `nurseries` | "Add Nursery" → `NurseryForm` | Nursery Registration Screen |
| Update nursery | `PUT /api/v1/nurseries/{id}` | `nurseries` | `NurseryDetailPanel` → "Edit" | Profile tab → "Edit Nursery" |
| Update status (Approve/Reject/Suspend) | `PUT /api/v1/nurseries/{id}/status` | `nurseries`, `audit_logs` | `NurseryDetailPanel` → "Approve" / "Reject" / "Suspend" | — |
| Delete nursery | `DELETE /api/v1/nurseries/{id}` | `nurseries` | `NurseryDetailPanel` → "Delete" | — |
| List addresses | `GET /api/v1/nurseries/{id}/addresses` | `nursery_addresses` | `NurseryDetailPanel` → Addresses tab | — |
| Add address | `POST /api/v1/nurseries/{id}/addresses` | `nursery_addresses` | Address form | — |
| Update / Delete address | `PUT/DELETE /api/v1/nurseries/addresses/{addressId}` | `nursery_addresses` | Address row → "Edit" / "Delete" | — |
| List managers | `GET /api/v1/nurseries/{id}/managers` | `nursery_users`, `users` | `NurseryDetailPanel` → Managers tab | Members Screen |
| Add manager | `POST /api/v1/nurseries/{id}/managers` | `nursery_users` | Managers tab → "Add Manager" | — |
| Remove manager | `DELETE /api/v1/nurseries/{id}/managers/{userId}` | `nursery_users`, `audit_logs` | Managers tab → "Remove" | Members Screen → "Remove" |
| List connected drivers | `GET /api/v1/nurseries/{id}/drivers` | `nursery_drivers`, `drivers` | `NurseryDetailPanel` → Drivers tab | — |
| Connect driver | `POST /api/v1/nurseries/{id}/drivers` | `nursery_drivers` | Drivers tab → "Connect Driver" | Connections Screen |
| Approve driver | `POST /api/v1/nurseries/{id}/drivers/{driverUserId}/approve` | `nursery_drivers`, `audit_logs` | Drivers tab → "Approve" | — |

---

## 4. 📦 Inventory

### 📋 Business Rules
- Full physical inventory (stock ledger, ERP, opening/closing stock, stock transactions) is **NOT in scope for V1**.
- Only a simple soft-quantity reference exists per nursery.
- Only the owner manages quantities. Managers may view but not modify.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| View all inventory | ✅ | own only | own nursery | ❌ | ❌ |
| View by nursery | ✅ | own only | own nursery | ❌ | ❌ |
| View by plant | ✅ | own only | own nursery | ❌ | ❌ |
| **Create** inventory entry | ✅ | own only | ❌ | ❌ | ❌ |
| **Update** quantity | ✅ | own only | ❌ | ❌ | ❌ |
| **Delete** inventory entry | ✅ | own only | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List inventory | `GET /api/v1/inventory` | `nursery_inventory` | Inventory page | Inventory List Screen |
| View by nursery | `GET /api/v1/nurseries/{nurseryId}/inventory` | `nursery_inventory` | `NurseryDetailPanel` → Inventory tab | Inventory List Screen |
| View by plant | `GET /api/v1/plants/{plantId}/inventory` | `nursery_inventory` | `PlantDetailPanel` → Inventory tab | — |
| Create entry | `POST /api/v1/inventory` | `nursery_inventory` | "Add" → `InventoryForm` | Inventory Add Screen → "Add Plant" |
| Update quantity | `PUT /api/v1/inventory/{id}` | `nursery_inventory` | `InventoryForm` → "Update" | Inventory Detail → "Edit" |
| Delete entry | `DELETE /api/v1/inventory/{id}` | `nursery_inventory` | Inventory row → "Delete" | — |

---

## 5. 📝 Quotations

### 📋 Business Rules
- Internal Quotation: no customer required.
- Customer Quotation: customer required.
- Draft quotations are editable by creator.
- Only the nursery owner may delete a quotation.
- Completed quotations are read-only — cannot be deleted or edited.
- Buyer can accept or reject a customer quotation.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **Create** internal quotation | ❌ | ✅ | ✅ | ❌ | ❌ |
| **Create** customer quotation | ❌ | ✅ | ✅ | ❌ | ❌ |
| **View** quotations | ✅ (all) | own nursery | assigned only | ❌ | own only |
| **Edit** quotation (draft only) | ✅ | own nursery | own created | ❌ | ❌ |
| Assign manager to quotation | ✅ | own only | ❌ | ❌ | ❌ |
| **Approve** quotation | ✅ | ✅ | ✅ | ❌ | ❌ |
| Convert to order | ✅ | ✅ | ✅ | ❌ | ❌ |
| **Delete** quotation | ✅ | own only | ❌ | ❌ | ❌ |
| Delete completed quotation | ❌ | ❌ | ❌ | ❌ | ❌ |
| Customer accepts quotation | ❌ | ❌ | ❌ | ❌ | ✅ (own) |
| Customer rejects quotation | ❌ | ❌ | ❌ | ❌ | ✅ (own) |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List quotations | `GET /api/v1/quotations` | `quotations` | Quotations page | Quotation List Screen |
| View quotation | `GET /api/v1/quotations/{id}` | `quotations`, `quotation_items` | `QuotationDetailPanel` | Quotation Detail Screen |
| Create quotation | `POST /api/v1/quotations` | `quotations`, `quotation_items` | "Create Quotation" → `QuotationCreateDrawer` | Quotation Create Screen → "Create" |
| Update quotation | `PUT /api/v1/quotations/{id}` | `quotations`, `quotation_items` | `QuotationDetailPanel` → "Edit" | Quotation Detail → "Edit" |
| Delete quotation | `DELETE /api/v1/quotations/{id}` | `quotations` | `QuotationDetailPanel` → "Delete" | — |
| Assign manager | `POST /api/v1/quotations/{id}/assign-manager` | `quotations`, `audit_logs` | `QuotationDetailPanel` → "Assign Manager" dropdown | — |
| Approve quotation | `POST /api/v1/quotations/{id}/approve` | `quotations`, `audit_logs` | `QuotationDetailPanel` → "Approve" | Quotation Detail → "Approve" |
| Convert to order | `POST /api/v1/quotations/{id}/convert-to-order` | `quotations`, `orders`, `audit_logs` | `QuotationDetailPanel` → "Convert to Order" | Quotation Detail → "Convert to Order" |
| Customer accept | `POST /api/v1/quotations/{id}/buyer-accept` | `quotations` | — | Quotation Detail → "Accept" |
| Customer reject | `POST /api/v1/quotations/{id}/buyer-reject` | `quotations` | — | Quotation Detail → "Reject" |

---

## 6. 📦 Orders

### 📋 Business Rules
- Direct order or from quotation — both supported.
- Order items are editable ONLY during loading (LOADING_STARTED / LOADING_IN_PROGRESS).
- After LOADING_COMPLETED, order is locked — no edits by anyone.
- Orders are never hard-deleted — only soft-cancelled.
- Manager sees only orders they are personally assigned to — not all nursery orders.
- Manager cannot view customer mobile number or address on the order.
- Admin cannot create orders — business rules prohibit admin from participating in transactions.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **Create** direct order | ❌ | ✅ | ✅ | ❌ | ❌ |
| **Create** order from quotation | ❌ | ✅ | ✅ | ❌ | ❌ |
| **View** orders | ✅ (all) | own nursery | assigned only | ❌ | own only |
| View other manager's orders | ✅ | ✅ | ❌ | ❌ | ❌ |
| View customer details on order | ✅ | ✅ | ❌ | ❌ | ❌ |
| **Assign** loading responsibility | ❌ | ✅ | ❌ | ❌ | ❌ |
| **Assign** driver | ❌ | ✅ | ❌ | ❌ | ❌ |
| **Reopen** loading | ❌ | ✅ | ❌ | ❌ | ❌ |
| **Cancel** order | ✅ | ✅ | ❌ | ❌ | ❌ |
| **Hard delete** order | ❌ | ❌ | ❌ | ❌ | ❌ |
| Edit order items (during loading only) | ❌ | ✅ | ✅ (assigned) | ❌ | ❌ |
| Edit order after loading completed | ❌ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List orders | `GET /api/v1/orders` | `orders` | Orders page | Order List Screen |
| View order | `GET /api/v1/orders/{id}` | `orders`, `order_items` | `OrderDetailPanel` | Order Detail Screen |
| Create order | `POST /api/v1/orders` | `orders`, `order_items`, `public_code_sequences` | "Create Order" → `OrderCreateDrawer` | Order Create Screen → "Create Order" |
| Update status | `PUT /api/v1/orders/{id}/status` | `orders`, `audit_logs` | `OrderDetailPanel` → `OrderStatusForm` | Order Detail → status buttons |
| Assign manager | `PUT /api/v1/orders/{id}/assign-manager` | `orders`, `audit_logs` | `OrderDetailPanel` → "Assign Manager" | — |
| Assign driver | `PUT /api/v1/orders/{id}/assign-driver` | `orders`, `nursery_drivers`, `audit_logs` | `OrderDetailPanel` → "Assign Driver" | — |
| Cancel order | `POST /api/v1/orders/{id}/cancel` | `orders`, `audit_logs` | `OrderDetailPanel` → "Cancel Order" | — |
| List items | `GET /api/v1/orders/{id}/items` | `order_items`, `plants` | `OrderDetailPanel` → Items tab | Order Detail → items list |
| Add item (loading) | `POST /api/v1/orders/{id}/items` | `order_items`, `audit_logs` | "Add Item" (during loading) | Order Detail → "Add Item" |
| Update item (loading) | `PUT /api/v1/orders/{id}/items/{itemId}` | `order_items`, `audit_logs` | Item row → "Edit" (during loading) | — |
| Remove item (loading) | `DELETE /api/v1/orders/{id}/items/{itemId}` | `order_items`, `audit_logs` | Item row → "Remove" (during loading) | — |
| List dispatches by order | `GET /api/v1/orders/{orderId}/dispatches` | `dispatches` | `OrderDetailPanel` → Dispatches tab | — |

---

## 7. 🚚 Loading

### 📋 Business Rules
- Loading = physically packing plants onto a vehicle before dispatch.
- Only one responsible person at a time: the owner OR one assigned manager.
- Driver must be assigned and accepted BEFORE loading starts.
- Order items are editable during loading (add, remove, update quantities).
- Multiple loading photos are supported.
- Once loading is COMPLETED, the order is locked — no further edits by anyone.
- Nobody can change the driver after loading starts.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **Start** loading | ❌ | ✅ | ✅ (if assigned) | ❌ | ❌ |
| **Complete** loading | ❌ | ✅ | ✅ (if assigned) | ❌ | ❌ |
| Add / Remove / Update items | ❌ | ✅ | ✅ (if assigned) | ❌ | ❌ |
| **Upload** loading photos | ❌ | ✅ | ✅ (if assigned) | ❌ | ❌ |
| Change driver after loading starts | ❌ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Start loading | `PUT /api/v1/orders/{id}/status` → `LOADING_STARTED` | `orders`, `audit_logs` | `OrderDetailPanel` → "Start Loading" | Order Detail → "Start Loading" |
| Complete loading | `PUT /api/v1/orders/{id}/status` → `LOADING_COMPLETED` | `orders`, `audit_logs` | `OrderDetailPanel` → "Complete Loading" | Order Detail → "Complete Loading" |
| Upload loading photos | `POST /api/v1/attachments` | `attachments` | "Upload Photo" | Order Detail → "Upload Photo" |

---

## 8. 🚛 Dispatches & Trips

### 📋 Business Rules
- Dispatch starts ONLY after Loading Completed AND driver has accepted the trip.
- Once dispatched, the order is read-only.
- Driver may only accept/reject trips they are assigned to.
- Trip UUID/QR is generated by the nursery owner to share with the driver.
- Status flow: Assigned → Accepted → In Transit → Completed.
- Admin cannot create or start a dispatch.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **Create / Start** dispatch | ❌ | ✅ | ✅ | ❌ | ❌ |
| View dispatch | ✅ (all) | own nursery | own nursery | assigned | own orders |
| Update dispatch status | ✅ | own nursery | own nursery | assigned | ❌ |
| **Accept** trip | ❌ | ❌ | ❌ | ✅ | ❌ |
| **Reject** trip (not yet accept) | ❌ | ❌ | ❌ | ✅ | ❌ |
| Create trip event | ✅ | ❌ | ❌ | ✅ (assigned) | ❌ |
| Add dispatch item | ✅ | own nursery | own nursery | ❌ | ❌ |
| **Complete** delivery | ❌ | ❌ | ❌ | ✅ | ❌ |
| **Upload** delivery proof | ❌ | ❌ | ❌ | ✅ | ❌ |
| Generate Trip UUID/QR | ❌ | ✅ | ❌ | ❌ | ❌ |
| Public track by UUID (no auth) | ✅ | ✅ | ✅ | ✅ | ✅ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List dispatches | `GET /api/v1/dispatches` | `dispatches` | Dispatches page | Dispatch List Screen |
| View dispatch | `GET /api/v1/dispatches/{id}` | `dispatches`, `dispatch_items` | `DispatchDetailPanel` | Dispatch Detail Screen |
| View by code | `GET /api/v1/dispatches/code/{code}` | `dispatches` | — | Driver → scan dispatch code |
| Create dispatch | `POST /api/v1/dispatches` | `dispatches`, `public_code_sequences`, `audit_logs` | "Create Dispatch" → `DispatchForm` | Order Detail → "Start Dispatch" |
| Update status | `PUT /api/v1/dispatches/{id}/status` | `dispatches`, `audit_logs` | `DispatchStatusForm` | — |
| Accept trip | `POST /api/v1/dispatches/{id}/accept` | `dispatch_assignments`, `audit_logs` | — | Trip Preview → "Accept Trip" |
| Add dispatch item | `POST /api/v1/dispatches/{id}/items` | `dispatch_items` | `DispatchDetailPanel` → "Add Item" | — |
| Add trip event | `POST /api/v1/dispatches/{id}/trip-events` | `trip_events`, `audit_logs` | — | Dispatch Detail → status update |
| Upload delivery proof | `POST /api/v1/attachments` | `attachments` | — | Dispatch Detail → "Upload Proof" |
| Public track | `GET /api/v1/track/{uuid}` | `dispatches`, `trip_tracking_links` | — | Delivery tracking link (no login) |
| List by order | `GET /api/v1/orders/{orderId}/dispatches` | `dispatches` | `OrderDetailPanel` → Dispatches tab | — |

---

## 9. 📍 Live GPS Tracking

### 📋 Business Rules
- Only the assigned driver posts GPS location points.
- Live tracking enabled only during active dispatch.
- Owner and assigned manager can view live location of their nursery's trips.
- Customer can view live location of their own order's delivery.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **Post** GPS location | ✅ (code allows) | ❌ | ❌ | ✅ (own trip) | ❌ |
| View dispatch tracking (live) | ✅ | own nursery | assigned | ❌ | own order |
| View dispatch tracking (history) | ✅ | own nursery | assigned | ❌ | own order |
| View driver tracking | ✅ | ❌ | ❌ | own only | ❌ |
| View vehicle tracking | ✅ | ❌ | ❌ | ❌ | ❌ |

> Admin can also post GPS in the current code (tracking service allows ADMIN || DRIVER). This is not a business rules violation, just an admin tool for debugging/testing.

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Post location | `POST /api/v1/tracking` | `driver_locations`, `vehicle_locations` | — | Driver Dashboard → background GPS |
| Dispatch tracking history | `GET /api/v1/dispatches/{dispatchId}/tracking` | `driver_locations` | `TrackingPage` | — |
| Dispatch tracking latest | `GET /api/v1/dispatches/{dispatchId}/tracking/latest` | `driver_locations` | `TrackingPage` → live map | Dispatch Detail → live map |
| Driver tracking history | `GET /api/v1/drivers/{driverId}/tracking` | `driver_locations` | `TrackingPage` | — |
| Driver tracking latest | `GET /api/v1/drivers/{driverId}/tracking/latest` | `driver_locations` | — | — |
| Vehicle tracking history | `GET /api/v1/vehicles/{vehicleId}/tracking` | `vehicle_tracking` | `TrackingPage` | — |
| Vehicle tracking latest | `GET /api/v1/vehicles/{vehicleId}/tracking/latest` | `vehicle_locations` | — | — |

---

## 10. 📨 Invites

### 📋 Business Rules
- UUID / QR is the ONLY mechanism for joining a nursery or trip.
- NURSERY_ONBOARDING_INVITE (grants NURSERY_OWNER role) can ONLY be sent by Admin — prevents self-promotion.
- A nursery owner cannot accept a MANAGER_INVITE — conflicting roles.
- A manager cannot accept a NURSERY_ONBOARDING_INVITE — conflicting roles.

### Permissions

| Invite Type | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| Create `NURSERY_ONBOARDING_INVITE` | ✅ | ❌ | ❌ | ❌ | ❌ |
| Create `MANAGER_INVITE` | ✅ | ✅ (own) | ❌ | ❌ | ❌ |
| Create `DRIVER_INVITE` | ✅ | ✅ | ✅ | ❌ | ❌ |
| Create `CUSTOMER_INVITE` | ✅ | ✅ | ✅ | ❌ | ❌ |
| Create `TRIP_SHARE_INVITE` | ✅ | ✅ | ✅ | ❌ | ❌ |
| Accept any invite | ✅ | ✅ | ✅ | ✅ | ✅ |
| Cancel invite (own created) | ✅ | own only | own only | ❌ | ❌ |
| List nursery invites | ✅ | own only | ❌ | ❌ | ❌ |

> **Accept side-effects:** `MANAGER_INVITE` accepted → inserts `nursery_users` row. `NURSERY_ONBOARDING_INVITE` accepted → grants NURSERY_OWNER role in `user_roles`.

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Create invite | `POST /api/v1/invites` | `invites` | Nursery → "Invite Manager" | Members Screen → "Invite" |
| View invite by UUID | `GET /api/v1/invites/{uuid}` | `invites` | — | Invite Accept Screen |
| Accept invite | `POST /api/v1/invites/{uuid}/accept` | `invites`, `nursery_users`, `user_roles`, `audit_logs` | — | Invite Accept Screen → "Accept" |
| Cancel invite | `POST /api/v1/invites/{uuid}/cancel` | `invites`, `audit_logs` | Invites tab → "Cancel" | Members → "Cancel Invite" |
| List nursery invites | `GET /api/v1/nurseries/{nurseryId}/invites` | `invites` | `NurseryDetailPanel` → Invites tab | Members Screen |
| QR scan | via QR scanner → resolve UUID | — | — | QR Scanner Screen → "Scan QR" |

---

## 11. 🔔 Notifications

### 📋 Business Rules
- Every important business event triggers a notification (order placed, loading started, dispatch, delivery, etc.).
- FCM push is mocked in V1 — real credentials needed for production.
- Each user manages their own notifications.
- Only Admin creates notifications and manages templates.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| View own notifications | ✅ | ✅ | ✅ | ✅ | ✅ |
| Mark read / delete own | ✅ | ✅ | ✅ | ✅ | ✅ |
| Mark all read | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Create** / send notification | ✅ | ❌ | ❌ | ❌ | ❌ |
| Manage templates (create / update / delete) | ✅ | ❌ | ❌ | ❌ | ❌ |
| List templates | ✅ | ❌ | ❌ | ❌ | ❌ |
| Register / update / delete device token | ✅ | ✅ | ✅ | ✅ | ✅ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List notifications | `GET /api/v1/notifications` | `notifications` | — | Notification List Screen → bell tab |
| Get notification | `GET /api/v1/notifications/{id}` | `notifications` | — | Notification item |
| Create notification | `POST /api/v1/notifications` | `notifications` | Admin → Send Notification | — |
| Mark read | `PUT /api/v1/notifications/{id}/read` | `notifications` | — | Notification → "Mark as Read" |
| Mark all read | `PUT /api/v1/notifications/read-all` | `notifications` | — | "Mark All Read" |
| Delete notification | `DELETE /api/v1/notifications/{id}` | `notifications` | — | Notification → swipe delete |
| List devices | `GET /api/v1/notifications/devices` | `user_notification_devices` | — | — |
| Register device | `POST /api/v1/notifications/devices` | `user_notification_devices` | — | App startup (silent) |
| Delete device | `DELETE /api/v1/notifications/devices/{id}` | `user_notification_devices` | — | Logout → deregister |
| List templates | `GET /api/v1/notifications/templates` | `notification_templates` | Admin → Templates | — |
| Create template | `POST /api/v1/notifications/templates` | `notification_templates` | Templates → "Add" | — |
| Update template | `PUT /api/v1/notifications/templates/{id}` | `notification_templates` | Template → "Edit" | — |
| Delete template | `DELETE /api/v1/notifications/templates/{id}` | `notification_templates` | Template → "Delete" | — |

---

## 12. 📎 Attachments

### 📋 Business Rules
- Used for loading photos, delivery proof photos, and nursery/plant images.
- Customers cannot upload or view internal operational attachments.
- Only Admin can delete attachments to preserve audit integrity.
- S3 file storage is mocked in V1 — pre-signed URL logic pending.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **Upload** attachment | ✅ | ✅ | ✅ | ✅ | ❌ |
| View / List attachments | ✅ | ✅ | ✅ | ✅ | ❌ |
| **Delete** attachment | ✅ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List attachments | `GET /api/v1/attachments` | `attachments` | Order/Dispatch → Attachments tab | Order Detail → Photos |
| Get attachment | `GET /api/v1/attachments/{id}` | `attachments` | Attachment preview | — |
| Upload attachment | `POST /api/v1/attachments` | `attachments` | "Upload" button | "Upload Photo" button |
| Delete attachment | `DELETE /api/v1/attachments/{id}` | `attachments` | Attachment → "Delete" | — |

---

## 13. 🗂️ Storage (Pre-signed Upload URLs)

### 📋 Business Rules
- Used to generate S3 pre-signed URLs for direct client-to-storage uploads.
- S3 is mocked in V1.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| Request pre-signed upload URL | ✅ | ✅ | ✅ | ✅ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Presign upload URL | `POST /api/v1/storage/presign` | — (S3 mock) | File upload flows | File upload flows |

---

## 14. 📋 Audit Logs

### 📋 Business Rules
- Every important business action must create an immutable audit record.
- Includes: login, nursery approval, manager join, quotation create/update, order create/update, loading start/complete, dispatch start, delivery complete, cancellation.
- Audit logs are immutable — no role may delete them, ever.
- Managers cannot view audit logs.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| **View** audit logs | ✅ | ✅ (own nursery) | ❌ | ❌ | ❌ |
| **Delete** audit logs | ❌ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List audit logs | `GET /api/v1/audit-logs` | `audit_logs` | Admin → Audit Logs page | Activity Screen |

---

## 15. 📊 Reports & Dashboard

### 📋 Business Rules
- Admin views platform-wide reports and statistics.
- Nursery owner views own nursery reports.
- Manager cannot view any reports — business rules explicitly prohibit this.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| Platform dashboard | ✅ | ❌ | ❌ | ❌ | ❌ |
| Platform users list | ✅ | ❌ | ❌ | ❌ | ❌ |
| Own nursery reports / dashboard | ✅ | ✅ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Platform dashboard | `GET /api/v1/admin/dashboard` | `orders`, `nurseries`, `users` | `DashboardPage` | Admin Dashboard Screen |
| Platform users | `GET /api/v1/admin/users` | `users`, `user_roles` | Admin → Users → `UserDetailPanel` | — |
| Owner dashboard | `GET /api/v1/me/owner-dashboard` | `nurseries`, `orders`, `quotations` | — | Owner Dashboard Screen |

---

## 16. 🌱 Plant Sourcing Network

### 📋 Business Rules
- Private B2B discovery network — NOT a marketplace, NOT inventory.
- Helps managers find nearby nurseries with the plants they need (reduces travel, phone calls).
- Managers have FULL access — they usually perform sourcing work on the owner's behalf.
- Default search radius: 50 km.
- Sourcing profile NEVER shows: customers, orders, quotations, managers, revenue, reports.
- Top 20 plants are NOT inventory — they indicate "we usually grow these" only.
- Sourcing posts NEVER create orders automatically.
- A nursery cannot respond to its own post.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| Join / leave network | ✅ (monitor) | ✅ | ✅ | ❌ | ❌ |
| Discover nearby nurseries | ✅ | ✅ | ✅ | ❌ | ❌ |
| Search by plant name | ✅ | ✅ | ✅ | ❌ | ❌ |
| View nursery sourcing profile | ✅ | ✅ | ✅ | ❌ | ❌ |
| Add / update / remove top 20 plants | ✅ | own nursery | own nursery | ❌ | ❌ |
| **Create** NEED / AVAILABLE post | ✅ | ✅ | ✅ | ❌ | ❌ |
| **Update** / delete own post | ✅ | own nursery | own nursery | ❌ | ❌ |
| **Respond** to a post | ✅ | other nurseries | other nurseries | ❌ | ❌ |
| Accept / decline a response | ✅ | post owner only | post owner only | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| Get membership | `GET /api/v1/nurseries/{nurseryId}/sourcing-membership` | `sourcing_network_members` | — | Sourcing → Network status |
| Join network | `POST /api/v1/nurseries/{nurseryId}/sourcing-membership` | `sourcing_network_members` | — | "Join Plant Sourcing Network" |
| Leave network | `DELETE /api/v1/nurseries/{nurseryId}/sourcing-membership` | `sourcing_network_members` | — | "Leave Network" |
| Discover nearby | `GET /api/v1/sourcing-network/nurseries` | `sourcing_network_members`, `nurseries` | — | Nearby Nurseries Screen |
| View nursery profile | `GET /api/v1/sourcing-network/nurseries/{nurseryId}` | `nurseries`, `nursery_featured_plants` | — | Nursery Profile Screen |
| List featured plants | `GET /api/v1/nurseries/{nurseryId}/featured-plants` | `nursery_featured_plants`, `plants` | — | Nursery Profile → "Top Plants" |
| Add featured plant | `POST /api/v1/nurseries/{nurseryId}/featured-plants` | `nursery_featured_plants` | — | "Add Top Plant" |
| Update featured plant | `PUT /api/v1/nurseries/{nurseryId}/featured-plants/{featuredId}` | `nursery_featured_plants` | — | Top Plant → "Edit" |
| Remove featured plant | `DELETE /api/v1/nurseries/{nurseryId}/featured-plants/{featuredId}` | `nursery_featured_plants` | — | Top Plant → "Remove" |
| List posts | `GET /api/v1/sourcing-posts` | `sourcing_posts`, `sourcing_post_photos` | — | Need / Available Plants Screens |
| Get post | `GET /api/v1/sourcing-posts/{id}` | `sourcing_posts` | — | Post Detail Screen |
| Create post | `POST /api/v1/sourcing-posts` | `sourcing_posts`, `public_code_sequences` | — | "Need Plant" / "Available Plant" |
| Update post | `PUT /api/v1/sourcing-posts/{id}` | `sourcing_posts` | — | My Posts → "Edit" |
| Delete post | `DELETE /api/v1/sourcing-posts/{id}` | `sourcing_posts` | — | My Posts → "Delete" |
| List responses | `GET /api/v1/sourcing-posts/{id}/responses` | `sourcing_post_responses` | — | Post Detail → "Responses" tab |
| Create response | `POST /api/v1/sourcing-posts/{id}/responses` | `sourcing_post_responses` | — | Post Detail → "Respond" |
| Update response (accept/decline) | `PUT /api/v1/sourcing-posts/{id}/responses/{responseId}` | `sourcing_post_responses` | — | Response → "Accept" / "Decline" |

---

## 17. 🚛 Drivers Module

### 📋 Business Rules
- Drivers are independent — they do not belong to any nursery.
- A driver can work with multiple nurseries but only one active trip at a time.
- Drivers join trips via UUID / QR code.
- Driver applications must be approved by Admin before the driver can accept trips.
- Owner / Manager cannot view driver private details.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List all drivers | ✅ | ❌ | ❌ | ❌ | ❌ |
| View driver profile | ✅ | ❌ | ❌ | own only | ❌ |
| **Create** driver (admin) | ✅ | ❌ | ❌ | ❌ | ❌ |
| Self-apply as driver | ❌ | ❌ | ❌ | ✅ | ❌ |
| **Update** driver details | ✅ | ❌ | ❌ | ❌ | ❌ |
| Approve driver application | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Delete** driver | ✅ | ❌ | ❌ | ❌ | ❌ |
| View own driver profile | ❌ | ❌ | ❌ | ✅ | ❌ |
| Post own location | ❌ | ❌ | ❌ | ✅ | ❌ |
| View own trip history | ❌ | ❌ | ❌ | ✅ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List drivers | `GET /api/v1/drivers` | `drivers`, `users` | Drivers page | — |
| View driver | `GET /api/v1/drivers/{id}` | `drivers`, `vehicles` | `DriverDetailPanel` | — |
| View own driver profile | `GET /api/v1/drivers/me` | `drivers` | — | Driver Screen |
| Admin create driver | `POST /api/v1/drivers` | `drivers`, `users`, `user_roles` | "Add Driver" → `DriverForm` | — |
| Driver self-apply | `POST /api/v1/drivers/apply` | `drivers`, `user_roles` | — | Driver Registration Screen |
| Approve driver | `POST /api/v1/drivers/{id}/approve` | `drivers`, `audit_logs` | `DriverDetailPanel` → "Approve" | — |
| Update driver | `PUT /api/v1/drivers/{id}` | `drivers` | `DriverDetailPanel` → "Edit" | Driver Screen → "Edit Profile" |
| Delete driver | `DELETE /api/v1/drivers/{id}` | `drivers` | `DriverDetailPanel` → "Delete" | — |
| Post location | `POST /api/v1/drivers/{id}/location` | `driver_locations` | — | Driver Dashboard → GPS service |

---

## 18. 🚗 Vehicles

### 📋 Business Rules
- Vehicles are platform-level assets managed exclusively by Admin in V1.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List / View vehicles | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Create** vehicle | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Update** vehicle | ✅ | ❌ | ❌ | ❌ | ❌ |
| **Delete** vehicle | ✅ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List vehicles | `GET /api/v1/vehicles` | `vehicles` | Vehicles page | — |
| View vehicle | `GET /api/v1/vehicles/{id}` | `vehicles` | `VehicleDetailPanel` | — |
| Create vehicle | `POST /api/v1/vehicles` | `vehicles`, `public_code_sequences` | "Add Vehicle" → `VehicleForm` | — |
| Update vehicle | `PUT /api/v1/vehicles/{id}` | `vehicles` | `VehicleDetailPanel` → "Edit" | Driver Dashboard → "Update Vehicle" |
| Delete vehicle | `DELETE /api/v1/vehicles/{id}` | `vehicles` | `VehicleDetailPanel` → "Delete" | — |

---

## 19. 👤 Users & Profile

### 📋 Business Rules
- One mobile = one account.
- Every user has one profile.
- Managers cannot view customer mobile number or home address — privacy rule.
- Admin manages all users platform-wide.
- Users manage their own addresses and sessions.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| Manage all users platform-wide | ✅ | ❌ | ❌ | ❌ | ❌ |
| View any user by ID | ✅ | ❌ | ❌ | ❌ | ❌ |
| View own profile (`/users/me`) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Update own profile (`PUT /users/me`) | ✅ | ✅ | ✅ | ✅ | ✅ |
| View customer mobile / address | ✅ | ✅ | ❌ | ❌ | ❌ |
| Manage own addresses | ✅ | ✅ | ✅ | ✅ | ✅ |
| View own roles | ✅ | ✅ | ✅ | ✅ | ✅ |
| View own sessions | ✅ | ✅ | ✅ | ✅ | ✅ |
| View workspaces | ✅ | ✅ | ✅ | ✅ | ✅ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| View own profile | `GET /api/v1/users/me` | `users`, `user_roles` | Top-right → profile | Profile tab |
| Update own profile | `PUT /api/v1/users/me` | `users` | — | Profile → "Edit Profile" |
| View user by ID | `GET /api/v1/users/{id}` | `users`, `user_roles` | `UserDetailPanel` | — |
| Update user | `PUT /api/v1/users/{id}` | `users` | `UserDetailPanel` → "Edit" | — |
| List addresses | `GET /api/v1/users/{id}/addresses` | `user_addresses` | — | Profile → "Addresses" |
| Add address | `POST /api/v1/users/{id}/addresses` | `user_addresses` | — | Profile → "Add Address" |
| Update address | `PUT /api/v1/users/addresses/{addressId}` | `user_addresses` | — | Address → "Edit" |
| Delete address | `DELETE /api/v1/users/addresses/{addressId}` | `user_addresses` | — | Address → "Delete" |
| View roles | `GET /api/v1/users/{id}/roles` | `user_roles`, `roles` | `UserDetailPanel` → Roles tab | — |
| View sessions | `GET /api/v1/users/{id}/sessions` | `user_sessions` | `UserDetailPanel` → Sessions tab | Profile → "Active Sessions" |
| List all users | `GET /api/v1/admin/users` | `users`, `user_roles` | Admin → Users page | — |

---

## 20. 💳 Payments

### 📋 Business Rules
- Payment gateway (Razorpay/PayU) is mocked in V1 — manual recording only.
- Managers have no payment access — financial operations are owner-only.
- Customers can view their own payment records.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List all payments | ✅ | own nursery | ❌ | ❌ | own only |
| Get payment | ✅ | own nursery | ❌ | ❌ | own only |
| **Create** payment | ✅ | own nursery | ❌ | ❌ | ❌ |
| Update payment status | ✅ | own nursery | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List payments | `GET /api/v1/payments` | `payments` | Payments page | — |
| Get payment | `GET /api/v1/payments/{id}` | `payments` | `PaymentDetailPanel` | — |
| Create payment | `POST /api/v1/payments` | `payments`, `public_code_sequences` | "Record Payment" → `PaymentForm` | — |
| Update status | `PUT /api/v1/payments/{id}/status` | `payments`, `audit_logs` | `PaymentStatusForm` | — |
| Payments by order | `GET /api/v1/orders/{id}/payments` | `payments` | `OrderDetailPanel` → Payments tab | Order Detail → Payments tab |

---

## 21. 🔄 Subscriptions

### 📋 Business Rules
- Subscriptions are nursery-level — only the owner subscribes, renews, or cancels.
- Managers have no subscription access.
- Subscription plans are publicly readable.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List subscription plans | ✅ | ✅ | ✅ | ✅ | ✅ |
| Get plan | ✅ | ✅ | ✅ | ✅ | ✅ |
| List all subscriptions | ✅ | ❌ | ❌ | ❌ | ❌ |
| View own subscription | ✅ | ✅ (own) | ❌ | ❌ | ❌ |
| **Create** subscription | ✅ | ✅ (own) | ❌ | ❌ | ❌ |
| Renew subscription | ✅ | ✅ (own) | ❌ | ❌ | ❌ |
| Cancel subscription | ✅ | ✅ (own) | ❌ | ❌ | ❌ |
| Change status (admin override) | ✅ | ❌ | ❌ | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List plans | `GET /api/v1/subscription-plans` | `subscription_plans` | Subscriptions → Plans tab | — |
| Get plan | `GET /api/v1/subscription-plans/{id}` | `subscription_plans` | Plan detail | — |
| List subscriptions | `GET /api/v1/subscriptions` | `user_subscriptions` | Subscriptions page | — |
| View own subscription | `GET /api/v1/subscriptions/me` | `user_subscriptions` | — | Profile → Subscription |
| View subscription | `GET /api/v1/subscriptions/{id}` | `user_subscriptions` | `SubscriptionDetailPanel` | — |
| Create subscription | `POST /api/v1/subscriptions` | `user_subscriptions`, `public_code_sequences` | `SubscriptionCreateDrawer` → "Subscribe" | — |
| Update status | `PUT /api/v1/subscriptions/{id}/status` | `user_subscriptions` | `SubscriptionDetailPanel` → status | — |
| Renew subscription | `POST /api/v1/subscriptions/{id}/renew` | `user_subscriptions`, `payments` | `SubscriptionDetailPanel` → "Renew" | — |
| Cancel subscription | `POST /api/v1/subscriptions/{id}/cancel` | `user_subscriptions` | `SubscriptionDetailPanel` → "Cancel" | — |

---

## 22. 🌱 Plant Requests (B2B Sourcing Requests)

### 📋 Business Rules
- B2B sourcing requests between nurseries — not customer-facing.
- Managers can create and manage requests on behalf of their nursery.
- Drivers and Customers never see B2B sourcing activity.
- Responses must come from a different nursery than the requester.

### Permissions

| Action | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|:---:|:---:|:---:|:---:|:---:|
| List plant requests | ✅ | ✅ | ✅ | ❌ | ❌ |
| View plant request | ✅ | ✅ | ✅ | ❌ | ❌ |
| **Create** plant request | ✅ | own nursery | own nursery | ❌ | ❌ |
| **Update** plant request | ✅ | own nursery | own nursery | ❌ | ❌ |
| Update request status | ✅ | own nursery | own nursery | ❌ | ❌ |
| **Delete** plant request | ✅ | own nursery | own nursery | ❌ | ❌ |
| List responses | ✅ | ✅ | ✅ | ❌ | ❌ |
| Submit response (as supplier) | ✅ | own nursery | own nursery | ❌ | ❌ |
| Update / Accept response | ✅ | request owner | request owner | ❌ | ❌ |

### Implementation Reference

| Action | API Endpoint | DB Tables | 🖥️ Admin UI | 📱 Mobile Screen / Button |
|---|---|---|---|---|
| List requests | `GET /api/v1/plant-requests` | `plant_requests` | Requests page | Request List Screen |
| View request | `GET /api/v1/plant-requests/{id}` | `plant_requests`, `plant_request_responses` | `RequestDetailPanel` | Request Detail Screen |
| Create request | `POST /api/v1/plant-requests` | `plant_requests`, `public_code_sequences` | "Create Request" → `RequestCreateDrawer` | Request Create → "Create" |
| Update request | `PUT /api/v1/plant-requests/{id}` | `plant_requests` | `RequestDetailPanel` → "Edit" | Request Detail → "Edit" |
| Update status | `PUT /api/v1/plant-requests/{id}/status` | `plant_requests`, `audit_logs` | `RequestDetailPanel` → status | — |
| Delete request | `DELETE /api/v1/plant-requests/{id}` | `plant_requests` | `RequestDetailPanel` → "Delete" | — |
| List responses | `GET /api/v1/plant-requests/{id}/responses` | `plant_request_responses` | `RequestDetailPanel` → Responses tab | Request Detail → Responses |
| Submit response | `POST /api/v1/plant-requests/{id}/responses` | `plant_request_responses` | "Respond" button | "Respond" button |
| Update response | `PUT /api/v1/plant-requests/responses/{responseId}` | `plant_request_responses` | Response → "Accept" / "Reject" | — |

---

## 🚫 Universal — Nobody Can Do This

| # | Rule | Business Rule Source |
|---|---|---|
| 1 | Hard-delete any order | Orders are permanent business records |
| 2 | Hard-delete any quotation | Transaction history must be preserved |
| 3 | Delete audit logs | Immutable by design — compliance |
| 4 | Edit order items after Loading Completed | Order is locked permanently at that point |
| 5 | Change driver after loading starts | Dispatch integrity |
| 6 | Own more than one nursery | Core principle: one owner = one nursery |
| 7 | Be Manager and Owner simultaneously | Conflicting roles — invite is rejected |
| 8 | Create inventory transactions | Physical inventory out of scope V1 |
| 9 | Admin create quotations or orders | Admin cannot participate in business transactions |
| 10 | View another nursery's customers / orders / reports | Privacy between nurseries is absolute |
| 11 | Nursery respond to its own sourcing post | Cannot be both requester and supplier |
| 12 | Sourcing posts auto-create orders | Sourcing is discovery only — never transactional |

---

## 🔄 Order Lifecycle — Who Acts at Each Step

| # | Step | 👑 Admin | 🌳 Owner | 👨‍💼 Manager | 🚛 Driver | 🤝 Customer |
|---|---|:---:|:---:|:---:|:---:|:---:|
| 1 | Create Quotation / Direct Order | ❌ | ✅ | ✅ | ❌ | ❌ |
| 2 | Customer Accepts Quotation | ❌ | ❌ | ❌ | ❌ | ✅ |
| 3 | Convert Quotation to Order | ❌ | ✅ | ✅ | ❌ | ❌ |
| 4 | Assign Loading Responsibility | ❌ | ✅ | ❌ | ❌ | ❌ |
| 5 | Assign Driver | ❌ | ✅ | ❌ | ❌ | ❌ |
| 6 | Driver Accepts Trip | ❌ | ❌ | ❌ | ✅ | ❌ |
| 7 | Start Loading + Edit Items | ❌ | ✅ | ✅ (assigned) | ❌ | ❌ |
| 8 | Complete Loading → Order Locked | ❌ | ✅ | ✅ (assigned) | ❌ | ❌ |
| 9 | Start Dispatch | ❌ | ✅ | ✅ | ❌ | ❌ |
| 10 | In Transit — Live GPS | view | view | view (assigned) | ✅ post | view |
| 11 | Upload Delivery Proof | ❌ | ❌ | ❌ | ✅ | ❌ |
| 12 | Complete Delivery | ❌ | ❌ | ❌ | ✅ | ❌ |
| 13 | Archived / Completed | view | view | view (assigned) | view | view |

---

## 🗑️ Soft Delete Rules

| Entity | DB Table | Soft Delete | Hard Delete |
|---|---|:---:|:---:|
| Users | `users` | ✅ | ❌ |
| Nurseries | `nurseries` | ✅ | ❌ |
| Quotations | `quotations` | ✅ | ❌ |
| Orders | `orders` | ✅ | ❌ |
| Dispatches / Trips | `dispatches` | ✅ | ❌ |
| Attachments | `attachments` | ✅ | ❌ |
| Audit Logs | `audit_logs` | ❌ Never | ❌ Never |

---

*Based on `bussiness-rules.md` and `bussiness_rules2.md`. Cross-checked against all route and service files. Last updated: 2026-06-27.*
