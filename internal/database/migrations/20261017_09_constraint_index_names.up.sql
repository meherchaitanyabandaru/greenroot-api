-- =============================================================================
-- Migration 2026-10-17 009: constraint and index names follow the convention
-- =============================================================================
-- Generated from the catalog (standard D13, enforced by
-- tests/api/naming_test.go):
--   primary key  <table>_pkey          foreign key  <table>_<columns>_fkey
--   unique       <table>_<columns>_key check        ck_<table>_<columns>
--   index        idx_<table>_<columns> unique index uq_<table>_<columns>
-- Names now say what they protect after the column/table renames of 008.
-- No code refers to these names except uq_payments_provider_payment, which
-- already conformed. Renames are catalog-only.
-- =============================================================================
ALTER TABLE public.dispatch_assignments RENAME CONSTRAINT "da_assigned_by_fkey" TO dispatch_assignments_assigned_by_user_id_fkey;
ALTER TABLE public.dispatch_assignments RENAME CONSTRAINT "da_dispatch_id_fkey" TO dispatch_assignments_dispatch_id_fkey;
ALTER TABLE public.dispatch_assignments RENAME CONSTRAINT "da_driver_id_fkey" TO dispatch_assignments_driver_id_fkey;
ALTER TABLE public.dispatch_assignments RENAME CONSTRAINT "da_vehicle_id_fkey" TO dispatch_assignments_vehicle_id_fkey;
ALTER TABLE public.email_messages RENAME CONSTRAINT "chk_email_messages_status" TO ck_email_messages_status;
ALTER TABLE public.email_messages RENAME CONSTRAINT "fk_email_messages_template" TO email_messages_email_template_id_fkey;
ALTER TABLE public.email_messages RENAME CONSTRAINT "fk_email_messages_user" TO email_messages_user_id_fkey;
ALTER TABLE public.email_senders RENAME CONSTRAINT "email_sender_pkey" TO email_senders_pkey;
ALTER TABLE public.email_templates RENAME CONSTRAINT "uq_email_templates_code" TO email_templates_template_code_key;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "chk_market_report_reason" TO ck_market_ad_reports_reason;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "chk_market_report_status" TO ck_market_ad_reports_status;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "market_listing_reports_pkey" TO market_ad_reports_pkey;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "market_reports_listing_fkey" TO market_ad_reports_ad_id_fkey;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "market_reports_nursery_fkey" TO market_ad_reports_reported_by_nursery_id_fkey;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "market_reports_user_fkey" TO market_ad_reports_reported_by_user_id_fkey;
ALTER TABLE public.market_ad_reports RENAME CONSTRAINT "uq_market_listing_report" TO market_ad_reports_ad_id_reported_by_user_id_key;
ALTER TABLE public.market_ad_saves RENAME CONSTRAINT "market_listing_saves_pkey" TO market_ad_saves_pkey;
ALTER TABLE public.market_ad_saves RENAME CONSTRAINT "market_saves_listing_fkey" TO market_ad_saves_ad_id_fkey;
ALTER TABLE public.market_ad_saves RENAME CONSTRAINT "market_saves_nursery_fkey" TO market_ad_saves_nursery_id_fkey;
ALTER TABLE public.market_ad_saves RENAME CONSTRAINT "market_saves_user_fkey" TO market_ad_saves_saved_by_user_id_fkey;
ALTER TABLE public.market_ad_saves RENAME CONSTRAINT "uq_market_listing_save" TO market_ad_saves_ad_id_nursery_id_key;
ALTER TABLE public.market_ad_views RENAME CONSTRAINT "market_listing_views_pkey" TO market_ad_views_pkey;
ALTER TABLE public.market_ad_views RENAME CONSTRAINT "market_views_listing_fkey" TO market_ad_views_ad_id_fkey;
ALTER TABLE public.market_ad_views RENAME CONSTRAINT "market_views_nursery_fkey" TO market_ad_views_nursery_id_fkey;
ALTER TABLE public.market_ad_views RENAME CONSTRAINT "uq_market_listing_view" TO market_ad_views_ad_id_nursery_id_key;
ALTER TABLE public.market_ads RENAME CONSTRAINT "chk_market_listing_status" TO ck_market_ads_status;
ALTER TABLE public.market_ads RENAME CONSTRAINT "market_listings_created_by_fkey" TO market_ads_created_by_user_id_fkey;
ALTER TABLE public.market_ads RENAME CONSTRAINT "market_listings_listing_code_key" TO market_ads_ad_code_key;
ALTER TABLE public.market_ads RENAME CONSTRAINT "market_listings_nursery_fkey" TO market_ads_nursery_id_fkey;
ALTER TABLE public.market_ads RENAME CONSTRAINT "market_listings_pkey" TO market_ads_pkey;
ALTER TABLE public.market_ads RENAME CONSTRAINT "market_listings_plant_fkey" TO market_ads_plant_id_fkey;
ALTER TABLE public.market_ads RENAME CONSTRAINT "market_listings_updated_by_fkey" TO market_ads_updated_by_user_id_fkey;
ALTER TABLE public.market_enquiries RENAME CONSTRAINT "chk_market_enquiry_status" TO ck_market_enquiries_status;
ALTER TABLE public.market_enquiries RENAME CONSTRAINT "uq_market_enquiry" TO market_enquiries_ad_id_enquiring_nursery_id_key;
ALTER TABLE public.market_enquiry_messages RENAME CONSTRAINT "market_msg_enquiry_fkey" TO market_enquiry_messages_enquiry_id_fkey;
ALTER TABLE public.market_enquiry_messages RENAME CONSTRAINT "market_msg_nursery_fkey" TO market_enquiry_messages_sent_by_nursery_id_fkey;
ALTER TABLE public.market_enquiry_messages RENAME CONSTRAINT "market_msg_user_fkey" TO market_enquiry_messages_sent_by_user_id_fkey;
ALTER TABLE public.nursery_applications RENAME CONSTRAINT "nursery_apps_applicant_fkey" TO nursery_applications_applicant_user_id_fkey;
ALTER TABLE public.nursery_applications RENAME CONSTRAINT "nursery_apps_nursery_id_fkey" TO nursery_applications_nursery_id_fkey;
ALTER TABLE public.nursery_applications RENAME CONSTRAINT "nursery_apps_reviewed_by_fkey" TO nursery_applications_reviewed_by_user_id_fkey;
ALTER TABLE public.nursery_drivers RENAME CONSTRAINT "uq_nursery_driver" TO nursery_drivers_nursery_id_driver_user_id_key;
ALTER TABLE public.nursery_featured_plants RENAME CONSTRAINT "chk_featured_display_order" TO ck_nursery_featured_plants_display_order;
ALTER TABLE public.nursery_featured_plants RENAME CONSTRAINT "uq_nursery_featured_plant" TO nursery_featured_plants_nursery_id_plant_id_key;
ALTER TABLE public.nursery_inventory RENAME CONSTRAINT "uq_inventory" TO nursery_inventory_nursery_id_plant_id_size_id_key;
ALTER TABLE public.nursery_users RENAME CONSTRAINT "nursery_users_uq" TO nursery_users_nursery_id_user_id_nursery_role_id_key;
ALTER TABLE public.order_delivery_snapshots RENAME CONSTRAINT "chk_order_delivery_snapshot_latitude" TO ck_order_delivery_snapshots_latitude;
ALTER TABLE public.order_delivery_snapshots RENAME CONSTRAINT "chk_order_delivery_snapshot_location_source" TO ck_order_delivery_snapshots_location_source;
ALTER TABLE public.order_delivery_snapshots RENAME CONSTRAINT "chk_order_delivery_snapshot_longitude" TO ck_order_delivery_snapshots_longitude;
ALTER TABLE public.order_documents RENAME CONSTRAINT "order_documents_file_size_nonnegative" TO ck_order_documents_file_size;
ALTER TABLE public.order_documents RENAME CONSTRAINT "order_documents_unique_version" TO order_documents_order_id_version_key;
ALTER TABLE public.order_documents RENAME CONSTRAINT "order_documents_version_positive" TO ck_order_documents_version;
ALTER TABLE public.order_verifications RENAME CONSTRAINT "order_verifications_status_check" TO ck_order_verifications_status;
ALTER TABLE public.plant_category_mappings RENAME CONSTRAINT "pcm_category_id_fkey" TO plant_category_mappings_category_id_fkey;
ALTER TABLE public.plant_category_mappings RENAME CONSTRAINT "pcm_plant_id_fkey" TO plant_category_mappings_plant_id_fkey;
ALTER TABLE public.plant_category_mappings RENAME CONSTRAINT "plant_category_mapping_pkey" TO plant_category_mappings_pkey;
ALTER TABLE public.plant_names RENAME CONSTRAINT "uq_plant_language" TO plant_names_plant_id_language_id_key;
ALTER TABLE public.plant_request_responses RENAME CONSTRAINT "prr_request_id_fkey" TO plant_request_responses_request_id_fkey;
ALTER TABLE public.plant_request_responses RENAME CONSTRAINT "prr_responded_by_user_id_fkey" TO plant_request_responses_responded_by_user_id_fkey;
ALTER TABLE public.plant_request_responses RENAME CONSTRAINT "prr_supplier_nursery_id_fkey" TO plant_request_responses_supplier_nursery_id_fkey;
ALTER TABLE public.plant_request_responses RENAME CONSTRAINT "uq_request_supplier" TO plant_request_responses_request_id_supplier_nursery_id_key;
ALTER TABLE public.quotation_documents RENAME CONSTRAINT "quotation_documents_file_size_nonnegative" TO ck_quotation_documents_file_size;
ALTER TABLE public.quotation_documents RENAME CONSTRAINT "quotation_documents_unique_version" TO quotation_documents_quotation_id_version_key;
ALTER TABLE public.quotation_documents RENAME CONSTRAINT "quotation_documents_version_positive" TO ck_quotation_documents_version;
-- fk_quot_items_quotation duplicated quotation_items_quotation_id_fkey exactly (same column,
-- target and ON DELETE CASCADE); every write paid for two identical checks.
ALTER TABLE public.quotation_items DROP CONSTRAINT "fk_quot_items_quotation";
ALTER TABLE public.quotation_verifications RENAME CONSTRAINT "quotation_verifications_status_check" TO ck_quotation_verifications_status;
ALTER TABLE public.quotations RENAME CONSTRAINT "chk_quotation_status" TO ck_quotations_status;
ALTER TABLE public.quotations RENAME CONSTRAINT "chk_quotation_type" TO ck_quotations_quotation_type;
ALTER TABLE public.quotations RENAME CONSTRAINT "fk_quotations_converted_order" TO quotations_converted_order_id_fkey;
ALTER TABLE public.sms_dlt_entities RENAME CONSTRAINT "chk_sms_dlt_entities_operator" TO ck_sms_dlt_entities_dlt_operator;
ALTER TABLE public.sms_dlt_entities RENAME CONSTRAINT "chk_sms_dlt_entities_pe_id" TO ck_sms_dlt_entities_pe_id;
ALTER TABLE public.sms_dlt_entities RENAME CONSTRAINT "chk_sms_dlt_entities_status" TO ck_sms_dlt_entities_status;
ALTER TABLE public.sms_headers RENAME CONSTRAINT "chk_sms_headers_status" TO ck_sms_headers_status;
ALTER TABLE public.sms_headers RENAME CONSTRAINT "chk_sms_headers_type" TO ck_sms_headers_header_type;
ALTER TABLE public.sms_headers RENAME CONSTRAINT "uq_sms_headers_header" TO sms_headers_header_key;
ALTER TABLE public.sms_messages RENAME CONSTRAINT "chk_sms_messages_status" TO ck_sms_messages_status;
ALTER TABLE public.sms_messages RENAME CONSTRAINT "fk_sms_messages_template" TO sms_messages_sms_template_id_fkey;
ALTER TABLE public.sms_messages RENAME CONSTRAINT "fk_sms_messages_user" TO sms_messages_user_id_fkey;
ALTER TABLE public.sms_preferences RENAME CONSTRAINT "fk_sms_preferences_user" TO sms_preferences_user_id_fkey;
ALTER TABLE public.sms_templates RENAME CONSTRAINT "chk_sms_templates_category" TO ck_sms_templates_category;
ALTER TABLE public.sms_templates RENAME CONSTRAINT "chk_sms_templates_dlt_id" TO ck_sms_templates_dlt_template_id;
ALTER TABLE public.sms_templates RENAME CONSTRAINT "chk_sms_templates_status" TO ck_sms_templates_status;
ALTER TABLE public.sms_templates RENAME CONSTRAINT "fk_sms_templates_header" TO sms_templates_header_id_fkey;
ALTER TABLE public.sms_templates RENAME CONSTRAINT "uq_sms_templates_code" TO sms_templates_template_code_key;
ALTER TABLE public.sourcing_post_responses RENAME CONSTRAINT "uq_post_response" TO sourcing_post_responses_post_id_responder_nursery_id_key;
ALTER TABLE public.sourcing_posts RENAME CONSTRAINT "chk_sourcing_post_type" TO ck_sourcing_posts_post_type;
ALTER TABLE public.sourcing_posts RENAME CONSTRAINT "chk_sourcing_urgency" TO ck_sourcing_posts_urgency;
ALTER TABLE public.subscription_promos RENAME CONSTRAINT "subscription_promos_discount_type_check" TO ck_subscription_promos_discount_type;
ALTER TABLE public.trip_tracking_links RENAME CONSTRAINT "ttl_customer_user_id_fkey" TO trip_tracking_links_customer_user_id_fkey;
ALTER TABLE public.trip_tracking_links RENAME CONSTRAINT "ttl_dispatch_id_fkey" TO trip_tracking_links_dispatch_id_fkey;
ALTER TABLE public.users RENAME CONSTRAINT "chk_users_initial_activity" TO ck_users_initial_activity;
ALTER INDEX public."dispatches_trip_uuid_idx" RENAME TO uq_dispatches_trip_uuid;
ALTER INDEX public."uq_driver_one_active_trip" RENAME TO uq_dispatches_driver_user_id;
ALTER INDEX public."uq_email_sender_singleton" RENAME TO uq_email_senders_expr;
ALTER INDEX public."idx_market_saves_nursery" RENAME TO idx_market_ad_saves_nursery_id;
ALTER INDEX public."idx_market_listings_expires_at" RENAME TO idx_market_ads_expires_at;
ALTER INDEX public."idx_market_listings_nursery" RENAME TO idx_market_ads_nursery_id;
ALTER INDEX public."idx_market_listings_plant" RENAME TO idx_market_ads_plant_name;
ALTER INDEX public."idx_market_listings_status" RENAME TO idx_market_ads_status;
ALTER INDEX public."idx_nursery_apps_applicant" RENAME TO idx_nursery_applications_applicant_user_id;
ALTER INDEX public."idx_nursery_apps_nursery" RENAME TO idx_nursery_applications_nursery_id;
ALTER INDEX public."idx_nursery_apps_status" RENAME TO idx_nursery_applications_status;
ALTER INDEX public."idx_nursery_featured_nursery" RENAME TO idx_nursery_featured_plants_nursery_id_display_order;
ALTER INDEX public."idx_nursery_featured_plant" RENAME TO idx_nursery_featured_plants_plant_id;
ALTER INDEX public."idx_inventory_nursery" RENAME TO idx_nursery_inventory_nursery_id;
ALTER INDEX public."idx_inventory_plant" RENAME TO idx_nursery_inventory_plant_id;
ALTER INDEX public."idx_inventory_status" RENAME TO idx_nursery_inventory_status;
ALTER INDEX public."uq_manager_one_active_nursery" RENAME TO uq_nursery_users_user_id;
ALTER INDEX public."idx_order_documents_current" RENAME TO uq_order_documents_order_id;
ALTER INDEX public."idx_order_verifications_active" RENAME TO uq_order_verifications_order_id;
ALTER INDEX public."idx_otp_mobile_active" RENAME TO idx_otp_requests_mobile_purpose_expires_at;
ALTER INDEX public."idx_pcm_category" RENAME TO idx_plant_category_mappings_category_id;
ALTER INDEX public."idx_pcm_plant" RENAME TO idx_plant_category_mappings_plant_id;
ALTER INDEX public."idx_request_response_request" RENAME TO idx_plant_request_responses_request_id;
ALTER INDEX public."idx_request_response_supplier" RENAME TO idx_plant_request_responses_supplier_nursery_id;
ALTER INDEX public."idx_plant_request_nursery" RENAME TO idx_plant_requests_requesting_nursery_id;
ALTER INDEX public."idx_plant_request_plant" RENAME TO idx_plant_requests_plant_id;
ALTER INDEX public."idx_plant_request_status" RENAME TO idx_plant_requests_status;
ALTER INDEX public."idx_quotation_documents_current" RENAME TO uq_quotation_documents_quotation_id;
ALTER INDEX public."idx_quotation_verifications_active" RENAME TO uq_quotation_verifications_quotation_id;
ALTER INDEX public."idx_sec_audit_created_at" RENAME TO idx_security_audit_logs_created_at;
ALTER INDEX public."idx_sec_audit_event_type" RENAME TO idx_security_audit_logs_event_type;
ALTER INDEX public."idx_sec_audit_nursery_id" RENAME TO idx_security_audit_logs_nursery_id;
ALTER INDEX public."idx_sec_audit_request_id" RENAME TO idx_security_audit_logs_request_id;
ALTER INDEX public."idx_sec_audit_user_id" RENAME TO idx_security_audit_logs_user_id;
ALTER INDEX public."idx_sourcing_members_active" RENAME TO idx_sourcing_network_members_nursery_id;
ALTER INDEX public."idx_sourcing_members_nursery" RENAME TO idx_sourcing_network_members_nursery_id_2;
ALTER INDEX public."idx_sourcing_photos_post" RENAME TO idx_sourcing_post_photos_post_id;
ALTER INDEX public."idx_sourcing_responses_nursery" RENAME TO idx_sourcing_post_responses_responder_nursery_id;
ALTER INDEX public."idx_sourcing_responses_post" RENAME TO idx_sourcing_post_responses_post_id;
ALTER INDEX public."idx_tracking_links_dispatch" RENAME TO idx_trip_tracking_links_dispatch_id;
ALTER INDEX public."idx_tracking_links_uuid" RENAME TO idx_trip_tracking_links_tracking_uuid;
