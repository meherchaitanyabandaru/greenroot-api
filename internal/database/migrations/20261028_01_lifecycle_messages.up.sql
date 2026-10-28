-- Order-lifecycle messages (2026-10-28). Customers who have never signed in
-- get the news by SMS (app users get it by push); money changes, nursery
-- approval, trial end and delivery problems are sent by SMS to everyone.
-- Each SMS body must be registered on DLT exactly as written
-- (greenroot-infra docs/sms.md) before going live.
INSERT INTO public.sms_templates (template_code, template_name, category, header_id, body, variables)
SELECT t.code, t.name, 'SERVICE_IMPLICIT', h.header_id, t.body, t.vars::jsonb
FROM (SELECT min(header_id) AS header_id FROM public.sms_headers) h,
(VALUES
  ('QUOTATION_RECEIVED', 'Quotation for a customer not on the app',
   'You have a GreenRoot quotation QUO-{#numeric#}-{#numeric#} for Rs.{#numeric#}.{#numeric#}. Sign in to the GreenRoot app with this number to view and accept it. - GreenRoot',
   '["quotation_date", "quotation_seq", "amount_rupees", "amount_paise"]'),
  ('ORDER_PLACED', 'Order placed for a customer not on the app',
   'Your GreenRoot order ORD-{#numeric#}-{#numeric#} for Rs.{#numeric#}.{#numeric#} has been placed. Sign in to the GreenRoot app with this number to track it. - GreenRoot',
   '["order_date", "order_seq", "amount_rupees", "amount_paise"]'),
  ('DELIVERY_ON_THE_WAY', 'On the way, with the delivery code',
   'Your GreenRoot delivery DSP-{#numeric#}-{#numeric#} is on the way. Give code {#numeric#} to the driver only after you check your plants. - GreenRoot',
   '["dispatch_date", "dispatch_seq", "delivery_code"]'),
  ('ORDER_PARTIAL', 'Order loaded with fewer plants',
   'GreenRoot order ORD-{#numeric#}-{#numeric#} was loaded with fewer plants than ordered. New total Rs.{#numeric#}.{#numeric#}. - GreenRoot',
   '["order_date", "order_seq", "amount_rupees", "amount_paise"]'),
  ('NURSERY_APPROVED', 'Nursery approved',
   'Your nursery is approved on GreenRoot. Sign in to start taking orders. - GreenRoot',
   '[]'),
  ('TRIAL_ENDING', 'Free trial ending',
   'Your GreenRoot free trial ends in {#numeric#} days. Choose a plan in the app to keep taking orders. - GreenRoot',
   '["days_left"]'),
  ('DELIVERY_ISSUE', 'Customer reported a delivery problem',
   'A customer reported a problem with GreenRoot delivery DSP-{#numeric#}-{#numeric#}. Open the app to respond. - GreenRoot',
   '["dispatch_date", "dispatch_seq"]')
) AS t(code, name, body, vars)
ON CONFLICT (template_code) DO NOTHING;

INSERT INTO public.email_templates (template_code, template_name, subject, body_html, body_text) VALUES
  ('TRIAL_ENDING', 'Free trial ending', 'Your GreenRoot free trial ends in {{days_left}} days',
   $html$<p>Your GreenRoot free trial ends in <strong>{{days_left}} days</strong>.</p>
<p>Open the app and choose a plan to keep taking orders and quotations without a break.</p>$html$,
   $text$Your GreenRoot free trial ends in {{days_left}} days.

Open the app and choose a plan to keep taking orders and quotations without a break.$text$)
ON CONFLICT (template_code) DO NOTHING;
