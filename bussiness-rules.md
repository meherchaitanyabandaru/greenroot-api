# 🌱 GreenRoot V1 – Master Business Rules

## 📖 Purpose

This document defines the complete business rules for GreenRoot V1.

This is the **single source of truth** for:

* 🗄️ PostgreSQL Database
* ⚙️ Go Backend APIs
* 💻 React Admin Portal
* 📱 Flutter Android Mobile App

All implementations must follow these business rules.

---

# 👥 User Types

| 👤 User          | 📝 Description                                   |
| ---------------- | ------------------------------------------------ |
| 👑 Super Admin   | Manages the GreenRoot platform                   |
| 🌳 Nursery Owner | Owns one nursery and manages business operations |
| 👨‍💼 Manager    | Works under one nursery                          |
| 🚛 Driver        | Independent driver who joins trips               |
| 🤝 Customer      | Purchases plants from nurseries                  |

---

# 📱 General User Rules

| ✅ Rule          | 📝 Description                                  |
| --------------- | ----------------------------------------------- |
| 📲 Login        | Mobile Number + OTP                             |
| 👤 One Account  | One mobile number = One GreenRoot account       |
| 🪪 Profile      | Every user has one profile                      |
| 🗑️ Soft Delete | Never physically delete business data           |
| 🆔 UUID/QR      | Used for invitations and trip joining           |
| 📋 Audit        | Every important business action must be audited |

---

# 👑 Super Admin Rules

### ✅ Can Do

* Approve nurseries
* Reject nurseries
* Suspend nurseries
* Activate nurseries
* Manage users
* View all audit logs
* Manage plant master
* Manage system settings
* View reports

### ❌ Cannot Do

* Create quotations
* Create orders
* Participate in business transactions

---

# 🌳 Nursery Owner Rules

### ✅ Can Do

* Register one nursery
* Update nursery profile
* Create Internal Quotations
* Create Customer Quotations
* Create Orders
* View Orders
* View Quotations
* Invite Managers using UUID/QR
* Generate Customer UUID
* Assign Loading Responsibility
* Assign Driver
* Generate Trip UUID
* Start Dispatch
* Track Trips
* View Customer Details
* View Reports
* View Audit Logs

### ❌ Cannot Do

* Own multiple nurseries
* Share nursery ownership
* Delete completed orders
* Delete completed quotations
* Edit locked orders
* Bypass nursery approval

---

# 🌳 Nursery Rules

| ✅ Rule             | 📝 Description                          |
| ------------------ | --------------------------------------- |
| 👤 One Owner       | One nursery has only one owner          |
| 🏢 One Nursery     | One owner owns only one nursery         |
| ✔️ Approval        | Nursery must be approved by Super Admin |
| ❌ Shared Ownership | Not supported                           |
| ❌ Multiple Owners  | Not supported                           |

---

# 👨‍💼 Manager Rules

### ✅ Can Do

* Join nursery using UUID/QR
* Create quotations
* Create orders
* Update orders
* Add items during loading
* Remove items during loading
* Update quantities
* Upload loading photos
* Start loading
* Complete loading
* Track assigned trips

### ❌ Cannot Do

* Own nursery
* Delete quotations
* Delete orders
* Cancel orders
* View customer mobile number
* View customer address
* View reports
* View audit logs
* View other managers' orders
* Change driver after loading starts

---

# 👨‍💼 Manager Membership Rules

| ✅ Rule            | 📝 Description                                |
| ----------------- | --------------------------------------------- |
| 🏢 Active Nursery | Manager can belong to only one active nursery |
| 🆔 Join Method    | UUID or QR                                    |
| 📌 Status         | Active, Suspended, Removed                    |
| 👑 Ownership      | Never becomes owner automatically             |

---

# 🚛 Driver Rules

### ✅ Can Do

* Login independently
* Update vehicle details
* Join trip using UUID/QR
* Accept trip
* Reject trip
* Share live GPS
* Upload delivery proof
* Complete delivery
* View trip history

### ❌ Cannot Do

* Create quotations
* Create orders
* Edit orders
* Edit quantities
* View customer details
* View reports
* View quotations
* View plant requests

---

# 🚛 Driver Rules

| ✅ Rule                | 📝 Description                        |
| --------------------- | ------------------------------------- |
| 🚛 Independent        | Driver does not belong to any nursery |
| 🌳 Multiple Nurseries | Driver can work with many nurseries   |
| 🚚 Active Trip        | Only one active trip at a time        |
| 🆔 Join Method        | Trip UUID or QR                       |

---

# 🤝 Customer Rules

### ✅ Can Do

* Join using Customer UUID
* View quotations
* View orders
* Track delivery
* View delivery proof
* Register own nursery later

### ❌ Cannot Do

* Create selling orders
* Edit quotations
* Edit orders
* View manager details
* View driver private details
* View audit logs
* View internal nursery operations

---

# 🤝 Customer Rules

| ✅ Rule                    | 📝 Description                          |
| ------------------------- | --------------------------------------- |
| 👤 Independent            | Customer has one GreenRoot account      |
| 🤝 Relationship           | Linked to one or more nurseries         |
| 🌳 Upgrade                | Customer can later become Nursery Owner |
| 🔗 Multiple Relationships | Can buy from multiple nurseries         |

---

# 📝 Quotation Rules

| ✅ Rule                | 📝 Description        |
| --------------------- | --------------------- |
| 📄 Internal Quotation | Customer not required |
| 👤 Customer Quotation | Customer required     |
| ✏️ Draft              | Editable              |
| 🗑️ Delete            | Owner only            |
| 🔄 Convert            | Can convert to Order  |
| 🗑️ Soft Delete       | Required              |

---

# 📦 Order Rules

| ✅ Rule            | 📝 Description          |
| ----------------- | ----------------------- |
| 🆕 Direct Order   | Supported               |
| 📄 From Quotation | Supported               |
| ✏️ Editable       | Until Loading Completed |
| 🔒 Locked         | After Loading Completed |
| 🔄 Reopen         | Owner only (optional)   |

---

# 🚚 Loading Rules

| ✅ Rule                     | 📝 Description          |
| -------------------------- | ----------------------- |
| 👤 Responsible Person      | Owner or one Manager    |
| 1️⃣ One Responsible Person | Only one at a time      |
| 🚛 Driver Required         | Before loading starts   |
| ✏️ Order Editing           | Allowed during loading  |
| 📸 Photos                  | Multiple loading photos |
| 📋 Audit                   | Every change is logged  |

---

# 🚛 Dispatch Rules

| ✅ Rule           | 📝 Description                 |
| ---------------- | ------------------------------ |
| 🚚 Dispatch      | Starts after Loading Completed |
| 👨‍✈️ Driver     | Must accept trip               |
| 📍 Live Tracking | Enabled                        |
| 🔒 Order         | Read-only                      |

---

# 📦 Delivery Rules

| ✅ Rule           | 📝 Description           |
| ---------------- | ------------------------ |
| 📸 Proof         | Required                 |
| 📷 Photos        | Multiple supported       |
| ✅ Completion     | Driver confirms delivery |
| 🔔 Notifications | Owner and Customer       |

---

# 🗺️ Trip Rules

| ✅ Rule       | 📝 Description                               |
| ------------ | -------------------------------------------- |
| 🆔 UUID      | Unique                                       |
| 📷 QR        | Supported                                    |
| 👨‍✈️ Driver | One active driver                            |
| 📊 Status    | Assigned → Accepted → In Transit → Completed |

---

# 🔐 Privacy Rules

| 👤 User          | 👀 Can See                     |
| ---------------- | ------------------------------ |
| 🌳 Nursery Owner | Everything in own nursery      |
| 👨‍💼 Manager    | Assigned operational data only |
| 🚛 Driver        | Assigned trip only             |
| 🤝 Customer      | Own orders only                |

---

# 🚫 Inventory Rules

| ❌ Rule                | 📝 Description      |
| --------------------- | ------------------- |
| 📦 Physical Inventory | Not Supported       |
| 📊 Stock Ledger       | Not Supported       |
| 📥 Opening Stock      | Not Supported       |
| 📤 Closing Stock      | Not Supported       |
| 📈 Stock Transactions | Not Supported       |
| 🏭 ERP Inventory      | Out of Scope for V1 |

---

# 📋 Audit Rules

Every important business action must create an immutable audit record.

Examples include:

* Login
* Nursery Approval
* Manager Join
* Customer Join
* Driver Join
* Quotation Create
* Quotation Update
* Order Create
* Order Update
* Loading Start
* Loading Complete
* Dispatch Start
* Delivery Complete
* Cancellation
* Loading Reopen

---

# 🗑️ Soft Delete Rules

| 📂 Entity      | 🗑️ Soft Delete |
| -------------- | --------------- |
| 👤 Users       | ✅               |
| 🌳 Nurseries   | ✅               |
| 📝 Quotations  | ✅               |
| 📦 Orders      | ✅               |
| 🚚 Trips       | ✅               |
| 🚛 Dispatches  | ✅               |
| 📎 Attachments | ✅               |
| 📋 Audit Logs  | ❌ Never Delete  |

---

# 🔄 Order Lifecycle

| #   | 📌 Step                                                       |
| --- | ------------------------------------------------------------- |
| 1️⃣ | Create Internal Quotation / Customer Quotation / Direct Order |
| 2️⃣ | Assign Loading Responsibility                                 |
| 3️⃣ | Assign Driver & Driver Accepts                                |
| 4️⃣ | Loading Started & Order Editing                               |
| 5️⃣ | Loading Completed & Order Locked                              |
| 6️⃣ | Dispatch Started                                              |
| 7️⃣ | In Transit                                                    |
| 8️⃣ | Delivered                                                     |
| 9️⃣ | Completed & Archived                                          |

---

# ⭐ Core Principles

1. 🌳 One Nursery = One Owner.
2. 👨‍💼 One Manager = One Active Nursery.
3. 🚛 Drivers are independent.
4. 🤝 Customers are independent.
5. ❌ No shared nursery ownership.
6. ❌ No physical inventory management in V1.
7. ✏️ Orders remain editable until Loading Completed.
8. 🔒 Completed business records are read-only.
9. 🗑️ Never physically delete business records.
10. 📋 Every important action must be audited.
11. 🆔 UUID/QR is the standard mechanism for invitations and trip joining.
12. 🗄️ Database stores the truth, ⚙️ API enforces business rules, and 📱 UI displays only permitted actions.
