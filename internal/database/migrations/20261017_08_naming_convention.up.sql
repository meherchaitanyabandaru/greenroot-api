-- =============================================================================
-- Migration 2026-10-17 008: one naming convention (standard D13)
-- =============================================================================
-- Renames columns and tables to the convention enforced by
-- tests/api/naming_test.go:
--   * a table's main lifecycle column is `status` (orders.order_status → status)
--   * timestamps end in _at (orders.order_date → ordered_at)
--   * booleans start with is_/has_/… (users.email_verified → is_email_verified)
--   * actor columns are <verb>_by_user_id (nurseries.approved_by → approved_by_user_id)
--   * primary keys are <entity>_id (invites.id → invite_id)
--   * table names are plural (email_sender → email_senders)
-- The API contract does not change: JSON fields and query parameters keep
-- their names; only SQL inside the API was updated (checked by cmd/sqlcheck).
-- No production deployment exists, so the old names are not needed by any
-- running binary (otherwise this would be an expand/contract sequence).
-- RENAME is a catalog change: no table rewrite, indexes and FKs follow.
-- =============================================================================

-- orders.created_by_user_id was NULL on every row; the creator is in
-- orders.created_by, which takes its name below.
ALTER TABLE public.orders DROP COLUMN created_by_user_id;

ALTER TABLE public.attachments RENAME COLUMN uploaded_by TO uploaded_by_user_id;
ALTER TABLE public.audit_logs RENAME COLUMN changed_by TO changed_by_user_id;
ALTER TABLE public.dispatch_assignments RENAME COLUMN assigned_by TO assigned_by_user_id;
ALTER TABLE public.dispatches RENAME COLUMN dispatched_by TO dispatched_by_user_id;
ALTER TABLE public.dispatches RENAME COLUMN owner_user_id_snapshot TO snapshot_owner_user_id;
ALTER TABLE public.dispatches RENAME COLUMN delivery_date TO delivered_at;
ALTER TABLE public.dispatches RENAME COLUMN dispatch_date TO dispatched_at;
ALTER TABLE public.dispatches RENAME COLUMN dispatch_status TO status;
ALTER TABLE public.driver_locations RENAME COLUMN created_by TO created_by_user_id;
ALTER TABLE public.email_sender RENAME COLUMN domain_verified TO is_domain_verified;
ALTER TABLE public.market_ads RENAME COLUMN pickup_confirmed_by TO pickup_confirmed_by_user_id;
ALTER TABLE public.notifications RENAME COLUMN notification_status TO status;
ALTER TABLE public.nurseries RENAME COLUMN approved_by TO approved_by_user_id;
ALTER TABLE public.nurseries RENAME COLUMN created_by TO created_by_user_id;
ALTER TABLE public.nurseries RENAME COLUMN rejected_by TO rejected_by_user_id;
ALTER TABLE public.nurseries RENAME COLUMN suspended_by TO suspended_by_user_id;
ALTER TABLE public.nurseries RENAME COLUMN unsuspended_by TO unsuspended_by_user_id;
ALTER TABLE public.nurseries RENAME COLUMN updated_by TO updated_by_user_id;
ALTER TABLE public.nursery_addresses RENAME COLUMN location_confirmed_by TO location_confirmed_by_user_id;
ALTER TABLE public.nursery_applications RENAME COLUMN reviewed_by TO reviewed_by_user_id;
ALTER TABLE public.nursery_drivers RENAME COLUMN connection_status TO status;
ALTER TABLE public.nursery_drivers RENAME COLUMN disconnected_by TO disconnected_by_user_id;
ALTER TABLE public.nursery_inventory RENAME COLUMN inventory_status TO status;
ALTER TABLE public.nursery_inventory RENAME COLUMN last_updated_by TO last_updated_by_user_id;
ALTER TABLE public.order_delivery_snapshots RENAME COLUMN confirmed_by TO confirmed_by_user_id;
ALTER TABLE public.order_delivery_snapshots RENAME COLUMN driver_acknowledged_by TO driver_acknowledged_by_user_id;
ALTER TABLE public.order_delivery_snapshots RENAME COLUMN emergency_updated TO is_emergency_update;
ALTER TABLE public.order_documents RENAME COLUMN generated_by TO generated_by_user_id;
ALTER TABLE public.order_verifications RENAME COLUMN revoked_by TO revoked_by_user_id;
ALTER TABLE public.orders RENAME COLUMN created_by TO created_by_user_id;
ALTER TABLE public.orders RENAME COLUMN updated_by TO updated_by_user_id;
ALTER TABLE public.orders RENAME COLUMN order_date TO ordered_at;
ALTER TABLE public.orders RENAME COLUMN order_status TO status;
ALTER TABLE public.payments RENAME COLUMN payment_date TO paid_at;
ALTER TABLE public.payments RENAME COLUMN payment_status TO status;
ALTER TABLE public.platform_config RENAME COLUMN updated_by TO updated_by_user_id;
ALTER TABLE public.quotation_documents RENAME COLUMN generated_by TO generated_by_user_id;
ALTER TABLE public.quotation_verifications RENAME COLUMN revoked_by TO revoked_by_user_id;
ALTER TABLE public.sms_dlt_entities RENAME COLUMN registration_status TO status;
ALTER TABLE public.sms_headers RENAME COLUMN registration_status TO status;
ALTER TABLE public.sms_preferences RENAME COLUMN promotional_opt_in TO is_promotional_opt_in;
ALTER TABLE public.sms_templates RENAME COLUMN registration_status TO status;
ALTER TABLE public.sourcing_network_members RENAME COLUMN contact_visible TO is_contact_visible;
ALTER TABLE public.sourcing_network_members RENAME COLUMN lorry_accessible TO is_lorry_accessible;
ALTER TABLE public.sourcing_network_members RENAME COLUMN road_accessible TO is_road_accessible;
ALTER TABLE public.subscription_promos RENAME COLUMN created_by TO created_by_user_id;
ALTER TABLE public.user_activities RENAME COLUMN activity_timestamp TO occurred_at;
ALTER TABLE public.user_addresses RENAME COLUMN location_confirmed_by TO location_confirmed_by_user_id;
ALTER TABLE public.user_roles RENAME COLUMN assigned_by TO assigned_by_user_id;
ALTER TABLE public.user_sessions RENAME COLUMN login_time TO logged_in_at;
ALTER TABLE public.user_sessions RENAME COLUMN session_status TO status;
ALTER TABLE public.user_subscriptions RENAME COLUMN auto_renew TO is_auto_renew;
ALTER TABLE public.user_subscriptions RENAME COLUMN subscription_status TO status;
ALTER TABLE public.users RENAME COLUMN created_by TO created_by_user_id;
ALTER TABLE public.users RENAME COLUMN email_verified TO is_email_verified;
ALTER TABLE public.users RENAME COLUMN mobile_verified TO is_mobile_verified;
ALTER TABLE public.users RENAME COLUMN onboarding_completed TO is_onboarding_completed;
ALTER TABLE public.users RENAME COLUMN suspended_by TO suspended_by_user_id;
ALTER TABLE public.users RENAME COLUMN unsuspended_by TO unsuspended_by_user_id;
ALTER TABLE public.users RENAME COLUMN updated_by TO updated_by_user_id;

ALTER TABLE public.invites RENAME COLUMN id TO invite_id;
ALTER TABLE public.nursery_drivers RENAME COLUMN id TO nursery_driver_id;
ALTER TABLE public.security_audit_logs RENAME COLUMN id TO security_audit_log_id;
ALTER TABLE public.trip_events RENAME COLUMN id TO trip_event_id;
ALTER TABLE public.trip_tracking_links RENAME COLUMN id TO trip_tracking_link_id;

ALTER TABLE public.email_sender RENAME TO email_senders;
ALTER TABLE public.plant_category_mapping RENAME TO plant_category_mappings;

-- The order's creator and last editor are users, like every other actor column.
ALTER TABLE public.orders ADD CONSTRAINT orders_created_by_user_id_fkey FOREIGN KEY (created_by_user_id) REFERENCES public.users(user_id);
ALTER TABLE public.orders ADD CONSTRAINT orders_updated_by_user_id_fkey FOREIGN KEY (updated_by_user_id) REFERENCES public.users(user_id);

