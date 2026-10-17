-- =============================================================================
-- Migration 2026-10-16 001: messaging core (SMS / TRAI DLT + email)
-- =============================================================================
-- For EXISTING databases (e.g. production). Fresh databases get the same
-- objects from schema.sql + seed-master.sql via scripts/reset-dbs.sh.
--
-- Idempotent: safe on databases that already have these objects (e.g. built
-- from an older schema.sql). Seed rows are inserted only if their code is
-- missing, so admin edits to templates are never overwritten.
-- =============================================================================

-- ── SMS (TRAI DLT) ───────────────────────────────────────────────────────────
-- TRAI DLT (TCCCPR 2018) SMS registry + outbound log.

-- The DLT Principal Entity (PE) registration. Singleton: one row per platform.
CREATE TABLE IF NOT EXISTS public.sms_dlt_entities (
    entity_id bigserial PRIMARY KEY,
    pe_id character varying(30),
    entity_name character varying(255) NOT NULL,
    dlt_operator character varying(30) DEFAULT 'JIO'::character varying NOT NULL,
    telemarketer_name character varying(255),
    telemarketer_id character varying(50),
    registration_status character varying(30) DEFAULT 'NOT_REGISTERED'::character varying NOT NULL,
    notes text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT chk_sms_dlt_entities_operator CHECK (((dlt_operator)::text = ANY (ARRAY['JIO'::text, 'AIRTEL'::text, 'VI'::text, 'BSNL'::text, 'TATA'::text, 'SMARTPING'::text]))),
    CONSTRAINT chk_sms_dlt_entities_status CHECK (((registration_status)::text = ANY (ARRAY['NOT_REGISTERED'::text, 'SUBMITTED'::text, 'APPROVED'::text, 'REJECTED'::text]))),
    CONSTRAINT chk_sms_dlt_entities_pe_id CHECK (((pe_id IS NULL) OR ((pe_id)::text ~ '^[0-9]{10,30}$'::text)))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_sms_dlt_entities_singleton ON public.sms_dlt_entities USING btree ((true));

-- Registered sender IDs (headers).
CREATE TABLE IF NOT EXISTS public.sms_headers (
    header_id bigserial PRIMARY KEY,
    header character varying(11) NOT NULL,
    header_type character varying(20) NOT NULL,
    dlt_header_id character varying(30),
    registration_status character varying(30) DEFAULT 'DRAFT'::character varying NOT NULL,
    rejection_reason text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_sms_headers_header UNIQUE (header),
    CONSTRAINT chk_sms_headers_type CHECK (((header_type)::text = ANY (ARRAY['TRANSACTIONAL'::text, 'SERVICE'::text, 'PROMOTIONAL'::text, 'GOVERNMENT'::text]))),
    CONSTRAINT chk_sms_headers_status CHECK (((registration_status)::text = ANY (ARRAY['DRAFT'::text, 'SUBMITTED'::text, 'APPROVED'::text, 'REJECTED'::text, 'INACTIVE'::text])))
);

-- Content templates exactly as registered on DLT ({#numeric#}, {#alphanumeric#}, ...).
-- variables: ordered JSON array of names, one per placeholder in body.
CREATE TABLE IF NOT EXISTS public.sms_templates (
    sms_template_id bigserial PRIMARY KEY,
    template_code character varying(60) NOT NULL,
    template_name character varying(255) NOT NULL,
    category character varying(30) NOT NULL,
    header_id bigint,
    body text NOT NULL,
    variables jsonb DEFAULT '[]'::jsonb NOT NULL,
    is_unicode boolean DEFAULT false NOT NULL,
    dlt_template_id character varying(30),
    registration_status character varying(30) DEFAULT 'DRAFT'::character varying NOT NULL,
    rejection_reason text,
    whitelisted_urls jsonb DEFAULT '[]'::jsonb NOT NULL,
    whitelisted_numbers jsonb DEFAULT '[]'::jsonb NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    approved_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_sms_templates_code UNIQUE (template_code),
    CONSTRAINT fk_sms_templates_header FOREIGN KEY (header_id) REFERENCES public.sms_headers(header_id),
    CONSTRAINT chk_sms_templates_category CHECK (((category)::text = ANY (ARRAY['TRANSACTIONAL'::text, 'SERVICE_IMPLICIT'::text, 'SERVICE_EXPLICIT'::text, 'PROMOTIONAL'::text]))),
    CONSTRAINT chk_sms_templates_status CHECK (((registration_status)::text = ANY (ARRAY['DRAFT'::text, 'SUBMITTED'::text, 'APPROVED'::text, 'REJECTED'::text, 'INACTIVE'::text]))),
    CONSTRAINT chk_sms_templates_dlt_id CHECK (((dlt_template_id IS NULL) OR ((dlt_template_id)::text ~ '^[0-9]{10,30}$'::text)))
);

-- Every outbound SMS attempt. OTP values are masked in message_body.
CREATE TABLE IF NOT EXISTS public.sms_messages (
    sms_message_id bigserial PRIMARY KEY,
    sms_template_id bigint,
    template_code character varying(60) NOT NULL,
    user_id bigint,
    mobile character varying(15) NOT NULL,
    message_body text NOT NULL,
    header character varying(11),
    dlt_entity_id character varying(30),
    dlt_template_id character varying(30),
    category character varying(30) NOT NULL,
    parts smallint DEFAULT 1 NOT NULL,
    provider character varying(30) NOT NULL,
    provider_message_id character varying(100),
    status character varying(20) NOT NULL,
    error_code character varying(60),
    error_message text,
    reference_type character varying(30),
    reference_id bigint,
    sent_at timestamp without time zone,
    delivered_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT fk_sms_messages_template FOREIGN KEY (sms_template_id) REFERENCES public.sms_templates(sms_template_id),
    CONSTRAINT fk_sms_messages_user FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE SET NULL,
    CONSTRAINT chk_sms_messages_status CHECK (((status)::text = ANY (ARRAY['QUEUED'::text, 'SENT'::text, 'DELIVERED'::text, 'FAILED'::text, 'BLOCKED'::text, 'MOCKED'::text])))
);
CREATE INDEX IF NOT EXISTS idx_sms_messages_mobile_created ON public.sms_messages USING btree (mobile, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_messages_status_created ON public.sms_messages USING btree (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sms_messages_template_created ON public.sms_messages USING btree (template_code, created_at DESC);

-- Per-user consent for consent-based SMS (PROMOTIONAL / SERVICE_EXPLICIT).
CREATE TABLE IF NOT EXISTS public.sms_preferences (
    user_id bigint PRIMARY KEY,
    promotional_opt_in boolean DEFAULT false NOT NULL,
    promotional_consent_at timestamp without time zone,
    promotional_revoked_at timestamp without time zone,
    consent_source character varying(30),
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT fk_sms_preferences_user FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE CASCADE
);

-- ── Email ────────────────────────────────────────────────────────────────────
-- Email channel of the messaging core.

-- Sender identity. Singleton.
CREATE TABLE IF NOT EXISTS public.email_sender (
    sender_id bigserial PRIMARY KEY,
    from_name character varying(100) DEFAULT 'GreenRoot'::character varying NOT NULL,
    from_address character varying(254),
    reply_to character varying(254),
    domain_verified boolean DEFAULT false NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_email_sender_singleton ON public.email_sender USING btree ((true));

-- Templates use {{snake_case}} placeholders. body_html is content only; the
-- API wraps it in the shared layout. Codes shared with SMS (PAYMENT_*) let
-- one messaging event reach both channels.
CREATE TABLE IF NOT EXISTS public.email_templates (
    email_template_id bigserial PRIMARY KEY,
    template_code character varying(60) NOT NULL,
    template_name character varying(255) NOT NULL,
    subject character varying(200) NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_text text DEFAULT ''::text NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT uq_email_templates_code UNIQUE (template_code)
);

-- Every outbound email attempt. Verification codes are masked in subject/body.
CREATE TABLE IF NOT EXISTS public.email_messages (
    email_message_id bigserial PRIMARY KEY,
    email_template_id bigint,
    template_code character varying(60) NOT NULL,
    user_id bigint,
    to_email character varying(254) NOT NULL,
    subject text NOT NULL,
    body text NOT NULL,
    provider character varying(30) NOT NULL,
    provider_message_id character varying(200),
    status character varying(20) NOT NULL,
    error_code character varying(60),
    error_message text,
    reference_type character varying(30),
    reference_id bigint,
    sent_at timestamp without time zone,
    delivered_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT fk_email_messages_template FOREIGN KEY (email_template_id) REFERENCES public.email_templates(email_template_id),
    CONSTRAINT fk_email_messages_user FOREIGN KEY (user_id) REFERENCES public.users(user_id) ON DELETE SET NULL,
    CONSTRAINT chk_email_messages_status CHECK (((status)::text = ANY (ARRAY['SENT'::text, 'DELIVERED'::text, 'BOUNCED'::text, 'FAILED'::text, 'BLOCKED'::text, 'MOCKED'::text])))
);
CREATE INDEX IF NOT EXISTS idx_email_messages_to_created ON public.email_messages USING btree (to_email, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_email_messages_status_created ON public.email_messages USING btree (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_email_messages_template_created ON public.email_messages USING btree (template_code, created_at DESC);

-- ── Seed: sender identities (singletons) ─────────────────────────────────────
INSERT INTO public.sms_dlt_entities (entity_name, dlt_operator, registration_status)
SELECT 'GreenRoot', 'JIO', 'NOT_REGISTERED'
WHERE NOT EXISTS (SELECT 1 FROM public.sms_dlt_entities);

INSERT INTO public.sms_headers (header, header_type, registration_status)
VALUES ('GRROOT', 'SERVICE', 'DRAFT')
ON CONFLICT (header) DO NOTHING;

INSERT INTO public.email_sender (from_name)
SELECT 'GreenRoot'
WHERE NOT EXISTS (SELECT 1 FROM public.email_sender);

-- ── Seed: SMS system templates (text must match DLT registration) ────────────
INSERT INTO public.sms_templates (template_code, template_name, category, header_id, body, variables)
SELECT v.code, v.name, 'SERVICE_IMPLICIT', h.header_id, v.body, v.vars::jsonb
FROM (VALUES
  ('AUTH_LOGIN_OTP', 'Login OTP',
   '{#numeric#} is your OTP to log in to GreenRoot. It is valid for {#numeric#} minutes. Do not share this OTP with anyone. - GreenRoot',
   '["otp", "valid_minutes"]'),
  ('AUTH_REGISTER_OTP', 'Registration OTP',
   '{#numeric#} is your OTP to create your GreenRoot account. It is valid for {#numeric#} minutes. Do not share this OTP with anyone. - GreenRoot',
   '["otp", "valid_minutes"]'),
  ('ACCOUNT_WELCOME', 'Account created',
   'Welcome to GreenRoot. Your account for the mobile number ending {#numeric#} has been created successfully. - GreenRoot',
   '["mobile_last4"]'),
  ('PAYMENT_RECEIVED_ORDER', 'Order payment received',
   'Payment of Rs.{#numeric#}.{#numeric#} received for GreenRoot order ORD-{#numeric#}-{#numeric#}. Payment ref PAY-{#numeric#}-{#numeric#}. - GreenRoot',
   '["amount_rupees", "amount_paise", "order_date", "order_seq", "payment_date", "payment_seq"]'),
  ('PAYMENT_RECEIVED_SUBSCRIPTION', 'Subscription payment received',
   'Payment of Rs.{#numeric#}.{#numeric#} received for your GreenRoot subscription. Payment ref PAY-{#numeric#}-{#numeric#}. - GreenRoot',
   '["amount_rupees", "amount_paise", "payment_date", "payment_seq"]'),
  ('PAYMENT_FAILED', 'Payment failed',
   'Your payment of Rs.{#numeric#}.{#numeric#} on GreenRoot (ref PAY-{#numeric#}-{#numeric#}) was not successful. Please try again. - GreenRoot',
   '["amount_rupees", "amount_paise", "payment_date", "payment_seq"]')
) AS v(code, name, body, vars)
CROSS JOIN (SELECT header_id FROM public.sms_headers WHERE header = 'GRROOT') h
ON CONFLICT (template_code) DO NOTHING;

-- ── Seed: email system templates ─────────────────────────────────────────────
INSERT INTO public.email_templates (template_code, template_name, subject, body_html, body_text) VALUES
  ('EMAIL_VERIFY_OTP', 'Email verification code', 'Verify your email for GreenRoot',
   $html$<p>Use this code to verify your email address on GreenRoot:</p>
<p style="font-size:30px;font-weight:bold;letter-spacing:6px;margin:16px 0;">{{otp}}</p>
<p>The code is valid for {{valid_minutes}} minutes. Never share it with anyone, including GreenRoot staff.</p>$html$,
   $text$Use this code to verify your email address on GreenRoot: {{otp}}

The code is valid for {{valid_minutes}} minutes. Never share it with anyone, including GreenRoot staff.$text$),

  ('PAYMENT_RECEIVED_ORDER', 'Order payment received', 'Payment received for order {{order_code}}',
   $html$<p>We have received your payment.</p>
<table role="presentation" cellpadding="6" cellspacing="0" style="border-collapse:collapse;margin:12px 0;">
<tr><td style="color:#6b7c72;">Amount</td><td style="font-weight:bold;">&#8377;{{amount}}</td></tr>
<tr><td style="color:#6b7c72;">Order</td><td>{{order_code}}</td></tr>
<tr><td style="color:#6b7c72;">Payment reference</td><td>{{payment_code}}</td></tr>
</table>
<p>You can see the order and its payments in the GreenRoot app.</p>$html$,
   $text$We have received your payment.

Amount: Rs.{{amount}}
Order: {{order_code}}
Payment reference: {{payment_code}}

You can see the order and its payments in the GreenRoot app.$text$),

  ('PAYMENT_RECEIVED_SUBSCRIPTION', 'Subscription payment received', 'Payment received for your GreenRoot subscription',
   $html$<p>We have received your subscription payment.</p>
<table role="presentation" cellpadding="6" cellspacing="0" style="border-collapse:collapse;margin:12px 0;">
<tr><td style="color:#6b7c72;">Amount</td><td style="font-weight:bold;">&#8377;{{amount}}</td></tr>
<tr><td style="color:#6b7c72;">Payment reference</td><td>{{payment_code}}</td></tr>
</table>
<p>Your plan details are in the GreenRoot app under Subscription.</p>$html$,
   $text$We have received your subscription payment.

Amount: Rs.{{amount}}
Payment reference: {{payment_code}}

Your plan details are in the GreenRoot app under Subscription.$text$),

  ('PAYMENT_FAILED', 'Payment failed', 'Your GreenRoot payment {{payment_code}} was not successful',
   $html$<p>Your payment of <strong>&#8377;{{amount}}</strong> (reference {{payment_code}}) was not successful.</p>
<p>Please try again from the GreenRoot app.</p>$html$,
   $text$Your payment of Rs.{{amount}} (reference {{payment_code}}) was not successful.

Please try again from the GreenRoot app.$text$)
ON CONFLICT (template_code) DO NOTHING;

