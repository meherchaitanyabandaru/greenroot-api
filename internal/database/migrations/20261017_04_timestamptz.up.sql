-- =============================================================================
-- Migration 2026-10-17 004: every timestamp is timestamptz (standard D1)
-- =============================================================================
-- Why: `timestamp without time zone` stores a wall clock with no zone. Go
-- (pgx) wrote the wall clock of whatever time.Time it was given, while
-- DEFAULT CURRENT_TIMESTAMP wrote the session's wall clock (IST locally), and
-- Go read everything back as UTC — so rows could be 5.5 h off depending on
-- who wrote them. timestamptz stores the instant; no writer can get it wrong.
--
-- USING col AT TIME ZONE 'UTC' interprets each stored wall clock as UTC,
-- which is exactly how the API has always read it, so every existing API
-- response is unchanged. The API pins its session time zone (DB_TIMEZONE,
-- default Asia/Kolkata) for CURRENT_DATE and date-based codes, and decodes
-- timestamptz as UTC.
--
-- 193 columns in 75 tables, generated from the catalog; one ALTER per table
-- so each table is rewritten once.
-- =============================================================================

ALTER TABLE public.attachments
    ALTER COLUMN uploaded_at TYPE timestamp with time zone USING uploaded_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC';

ALTER TABLE public.audit_logs
    ALTER COLUMN changed_at TYPE timestamp with time zone USING changed_at AT TIME ZONE 'UTC';

ALTER TABLE public.dispatch_assignments
    ALTER COLUMN assigned_at TYPE timestamp with time zone USING assigned_at AT TIME ZONE 'UTC';

ALTER TABLE public.dispatch_items
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.dispatches
    ALTER COLUMN dispatch_date TYPE timestamp with time zone USING dispatch_date AT TIME ZONE 'UTC',
    ALTER COLUMN delivery_date TYPE timestamp with time zone USING delivery_date AT TIME ZONE 'UTC',
    ALTER COLUMN trip_started_at TYPE timestamp with time zone USING trip_started_at AT TIME ZONE 'UTC',
    ALTER COLUMN completed_at TYPE timestamp with time zone USING completed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN driver_accepted_at TYPE timestamp with time zone USING driver_accepted_at AT TIME ZONE 'UTC',
    ALTER COLUMN driver_rejected_at TYPE timestamp with time zone USING driver_rejected_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC';

ALTER TABLE public.driver_locations
    ALTER COLUMN recorded_at TYPE timestamp with time zone USING recorded_at AT TIME ZONE 'UTC';

ALTER TABLE public.drivers
    ALTER COLUMN approved_at TYPE timestamp with time zone USING approved_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.email_messages
    ALTER COLUMN sent_at TYPE timestamp with time zone USING sent_at AT TIME ZONE 'UTC',
    ALTER COLUMN delivered_at TYPE timestamp with time zone USING delivered_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.email_sender
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.email_templates
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.invites
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN accepted_at TYPE timestamp with time zone USING accepted_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.languages
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.market_ad_reports
    ALTER COLUMN reviewed_at TYPE timestamp with time zone USING reviewed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.market_ad_saves
    ALTER COLUMN saved_at TYPE timestamp with time zone USING saved_at AT TIME ZONE 'UTC';

ALTER TABLE public.market_ad_views
    ALTER COLUMN viewed_at TYPE timestamp with time zone USING viewed_at AT TIME ZONE 'UTC';

ALTER TABLE public.market_ads
    ALTER COLUMN pickup_confirmed_at TYPE timestamp with time zone USING pickup_confirmed_at AT TIME ZONE 'UTC',
    ALTER COLUMN published_at TYPE timestamp with time zone USING published_at AT TIME ZONE 'UTC',
    ALTER COLUMN paused_at TYPE timestamp with time zone USING paused_at AT TIME ZONE 'UTC',
    ALTER COLUMN expired_at TYPE timestamp with time zone USING expired_at AT TIME ZONE 'UTC',
    ALTER COLUMN archived_at TYPE timestamp with time zone USING archived_at AT TIME ZONE 'UTC',
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.market_enquiries
    ALTER COLUMN viewed_at TYPE timestamp with time zone USING viewed_at AT TIME ZONE 'UTC',
    ALTER COLUMN replied_at TYPE timestamp with time zone USING replied_at AT TIME ZONE 'UTC',
    ALTER COLUMN closed_at TYPE timestamp with time zone USING closed_at AT TIME ZONE 'UTC',
    ALTER COLUMN cancelled_at TYPE timestamp with time zone USING cancelled_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.market_enquiry_messages
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.notification_templates
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.notifications
    ALTER COLUMN sent_at TYPE timestamp with time zone USING sent_at AT TIME ZONE 'UTC',
    ALTER COLUMN read_at TYPE timestamp with time zone USING read_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.nurseries
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN approved_at TYPE timestamp with time zone USING approved_at AT TIME ZONE 'UTC',
    ALTER COLUMN rejected_at TYPE timestamp with time zone USING rejected_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_addresses
    ALTER COLUMN location_confirmed_at TYPE timestamp with time zone USING location_confirmed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_applications
    ALTER COLUMN reviewed_at TYPE timestamp with time zone USING reviewed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_drivers
    ALTER COLUMN connected_at TYPE timestamp with time zone USING connected_at AT TIME ZONE 'UTC',
    ALTER COLUMN disconnected_at TYPE timestamp with time zone USING disconnected_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_featured_plants
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_inventory
    ALTER COLUMN last_updated_at TYPE timestamp with time zone USING last_updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_roles
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.nursery_users
    ALTER COLUMN joined_at TYPE timestamp with time zone USING joined_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.order_delivery_snapshots
    ALTER COLUMN confirmed_at TYPE timestamp with time zone USING confirmed_at AT TIME ZONE 'UTC',
    ALTER COLUMN driver_acknowledged_at TYPE timestamp with time zone USING driver_acknowledged_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.order_documents
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.order_items
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.order_verifications
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN revoked_at TYPE timestamp with time zone USING revoked_at AT TIME ZONE 'UTC';

ALTER TABLE public.orders
    ALTER COLUMN order_date TYPE timestamp with time zone USING order_date AT TIME ZONE 'UTC',
    ALTER COLUMN loading_started_at TYPE timestamp with time zone USING loading_started_at AT TIME ZONE 'UTC',
    ALTER COLUMN loading_completed_at TYPE timestamp with time zone USING loading_completed_at AT TIME ZONE 'UTC',
    ALTER COLUMN cancelled_at TYPE timestamp with time zone USING cancelled_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC';

ALTER TABLE public.otp_requests
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN used_at TYPE timestamp with time zone USING used_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.payments
    ALTER COLUMN payment_date TYPE timestamp with time zone USING payment_date AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_care_guides
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_categories
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_category_mapping
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_images
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_names
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_request_responses
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_requests
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN fulfilled_at TYPE timestamp with time zone USING fulfilled_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plant_sizes
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.plants
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.platform_config
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.public_code_sequences
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.quotation_documents
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.quotation_items
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.quotation_verifications
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN revoked_at TYPE timestamp with time zone USING revoked_at AT TIME ZONE 'UTC';

ALTER TABLE public.quotations
    ALTER COLUMN sent_at TYPE timestamp with time zone USING sent_at AT TIME ZONE 'UTC',
    ALTER COLUMN customer_responded_at TYPE timestamp with time zone USING customer_responded_at AT TIME ZONE 'UTC',
    ALTER COLUMN converted_at TYPE timestamp with time zone USING converted_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN valid_until TYPE timestamp with time zone USING valid_until AT TIME ZONE 'UTC';

ALTER TABLE public.roles
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.schema_migrations
    ALTER COLUMN applied_at TYPE timestamp with time zone USING applied_at AT TIME ZONE 'UTC';

ALTER TABLE public.sms_dlt_entities
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.sms_headers
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.sms_messages
    ALTER COLUMN sent_at TYPE timestamp with time zone USING sent_at AT TIME ZONE 'UTC',
    ALTER COLUMN delivered_at TYPE timestamp with time zone USING delivered_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.sms_preferences
    ALTER COLUMN promotional_consent_at TYPE timestamp with time zone USING promotional_consent_at AT TIME ZONE 'UTC',
    ALTER COLUMN promotional_revoked_at TYPE timestamp with time zone USING promotional_revoked_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.sms_templates
    ALTER COLUMN approved_at TYPE timestamp with time zone USING approved_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.sourcing_network_members
    ALTER COLUMN joined_at TYPE timestamp with time zone USING joined_at AT TIME ZONE 'UTC',
    ALTER COLUMN deactivated_at TYPE timestamp with time zone USING deactivated_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.sourcing_post_photos
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.sourcing_post_responses
    ALTER COLUMN responded_at TYPE timestamp with time zone USING responded_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.sourcing_posts
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN closed_at TYPE timestamp with time zone USING closed_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.subscription_plans
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.subscription_promos
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.trip_events
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.trip_tracking_links
    ALTER COLUMN expires_at TYPE timestamp with time zone USING expires_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.user_activities
    ALTER COLUMN activity_timestamp TYPE timestamp with time zone USING activity_timestamp AT TIME ZONE 'UTC';

ALTER TABLE public.user_addresses
    ALTER COLUMN location_confirmed_at TYPE timestamp with time zone USING location_confirmed_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.user_notification_devices
    ALTER COLUMN last_seen_at TYPE timestamp with time zone USING last_seen_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.user_roles
    ALTER COLUMN assigned_at TYPE timestamp with time zone USING assigned_at AT TIME ZONE 'UTC';

ALTER TABLE public.user_sessions
    ALTER COLUMN login_time TYPE timestamp with time zone USING login_time AT TIME ZONE 'UTC',
    ALTER COLUMN last_activity_at TYPE timestamp with time zone USING last_activity_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC';

ALTER TABLE public.user_subscriptions
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.users
    ALTER COLUMN onboarding_completed_at TYPE timestamp with time zone USING onboarding_completed_at AT TIME ZONE 'UTC',
    ALTER COLUMN last_login_at TYPE timestamp with time zone USING last_login_at AT TIME ZONE 'UTC',
    ALTER COLUMN deleted_at TYPE timestamp with time zone USING deleted_at AT TIME ZONE 'UTC',
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';

ALTER TABLE public.vehicle_locations
    ALTER COLUMN recorded_at TYPE timestamp with time zone USING recorded_at AT TIME ZONE 'UTC';

ALTER TABLE public.vehicle_tracking
    ALTER COLUMN tracked_at TYPE timestamp with time zone USING tracked_at AT TIME ZONE 'UTC';

ALTER TABLE public.vehicles
    ALTER COLUMN created_at TYPE timestamp with time zone USING created_at AT TIME ZONE 'UTC',
    ALTER COLUMN updated_at TYPE timestamp with time zone USING updated_at AT TIME ZONE 'UTC';
