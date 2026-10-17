-- Drops the messaging tables and every template, log row and preference in
-- them. Only for rolling back a failed deploy before real messages exist.
DROP TABLE IF EXISTS public.email_messages;
DROP TABLE IF EXISTS public.email_templates;
DROP TABLE IF EXISTS public.email_sender;
DROP TABLE IF EXISTS public.sms_messages;
DROP TABLE IF EXISTS public.sms_preferences;
DROP TABLE IF EXISTS public.sms_templates;
DROP TABLE IF EXISTS public.sms_headers;
DROP TABLE IF EXISTS public.sms_dlt_entities;
