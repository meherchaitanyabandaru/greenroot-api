# 🌱 GreenRoot V1 – Plant Sourcing Network

## 🎯 Purpose

The Plant Sourcing Network helps nursery owners and managers quickly locate plants from nearby nurseries.

It is designed to reduce the time spent travelling between villages and making phone calls.

This module is **not a marketplace**.

This module is **not inventory management**.

This module is a **private sourcing network** for approved nursery owners.

---

# 🌿 Real Business Problem

Today, when a customer places an order for plants, the nursery owner often does not have all required plants.

Example

Customer orders

* 200 Mango Trees
* 100 Coconut Plants
* 50 Neem Trees

The nursery owner asks the manager:

> "Go and bring these plants."

The manager

* takes petrol money
* travels by bike
* visits multiple villages
* calls many nursery owners

Typical questions

* Do you have Mango trees?
* How many?
* What size?
* What quality?
* Is your nursery close to the road?
* Can a lorry reach there?
* Is pickup possible today?

This process takes several hours.

The Plant Sourcing Network reduces this effort by helping users discover nearby nurseries that are likely to have the required plants.

---

# 🎯 Objective

Reduce plant sourcing time from hours to minutes.

Help nursery owners and managers

* discover nearby nurseries
* discover plant availability
* contact nearby owners
* reduce unnecessary travel
* improve sourcing efficiency

---

# 👥 Users

| User             | Access               |
| ---------------- | -------------------- |
| 👑 Super Admin   | Monitor              |
| 🌳 Nursery Owner | Full Access          |
| 👨‍💼 Manager    | Full Sourcing Access |
| 🚛 Driver        | No Access            |
| 🤝 Customer      | No Access            |

Unlike other modules,

Managers SHOULD have access.

Because managers usually perform sourcing work.

---

# ✅ Join Network

Participation is optional.

Owner enables

```text
Join Plant Sourcing Network
```

Only participating nurseries become visible.

---

# 📍 Nearby Nursery Discovery

Managers and owners can discover nearby nurseries.

Default radius

```text
50 KM
```

Nearby nurseries should be sorted by

* distance
* recently active
* matching plants

---

# 🏡 Nursery Profile

Only limited information is shown.

Show

* Nursery Name
* Village
* Distance
* Nursery Photos
* Road Accessibility
* Lorry Accessible (Yes/No)
* Contact Number (optional)
* Top Available Plants

Never show

* Customers
* Orders
* Quotations
* Reports
* Managers
* Financial information

---

# 🌳 Top Available Plants

Each nursery may publish its

Top 20 Available Plants.

These represent plants they usually grow or frequently have available.

Example

* Mango
* Coconut
* Neem
* Ashoka
* Teak

Each item may contain

* Plant
* Approximate Size
* Approximate Quantity
* Quality Notes
* Photos

Important

These are NOT inventory quantities.

These are NOT guaranteed stock.

They simply indicate

"We usually have these plants."

---

# 🔍 Smart Plant Search

Manager searches

```text
Mango
```

Results

Nearby nurseries

↓

Sorted by distance

↓

Shows

* Nursery
* Distance
* Road Access
* Top Plant Information
* Photos

Manager can immediately decide

which nursery to visit first.

---

# 🌱 Plant Requirement Post

If nearby search is insufficient,

Owner or Manager can create

Need Plant Post.

Example

```text
Need

200 Mango Trees

Large Size

Needed Today
```

Nearby participating nurseries receive notification.

---

# 🌳 Plant Availability Post

Owner may also publish

Availability Post.

Example

```text
Available

150 Coconut Plants

Pickup Today
```

Nearby owners receive notification.

---

# 🔔 Notifications

Notify nearby nurseries when

* New Need Post created
* New Availability Post created
* Someone responds

---

# 📱 Mobile Screens

* Join Plant Sourcing Network
* Nearby Nurseries
* Nursery Profile
* Plant Search
* Need Plants
* Available Plants
* My Posts
* Responses

---

# 🗄️ Database

## sourcing_network_members

Stores participating nurseries.

---

## nursery_featured_plants

Stores

Top 20 Available Plants

Not inventory.

Columns

* nursery_id
* plant_id
* display_order
* approximate_quantity
* approximate_size
* quality_notes
* photos

---

## sourcing_posts

Stores

Need

or

Available

posts.

---

## sourcing_post_responses

Stores responses.

---

## sourcing_post_photos

Stores post photos.

---

# 🚫 Important Business Rules

✅ This module never creates inventory.

✅ This module never guarantees stock.

✅ This module never exposes customer data.

✅ This module never exposes quotations.

✅ This module never creates customer orders automatically.

✅ This module simply helps managers and owners discover nearby nurseries and likely plant availability.

---

# 🎯 Success Criteria

The Plant Sourcing Network is successful if it helps a nursery owner or manager answer these questions within a few minutes:

* Which nearby nurseries probably have this plant?
* How far are they?
* Is the nursery near the road?
* What quality and size do they usually maintain?
* Can I call them before travelling?
* Which nursery should I visit first?

The goal is to reduce unnecessary travel, save petrol costs, save time, and make plant sourcing significantly more efficient while preserving the privacy of every nursery's business data.
