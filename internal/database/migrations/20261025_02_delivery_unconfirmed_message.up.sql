-- Customer is told by SMS and email (not only the in-app inbox) when a
-- nursery marks a trip delivered without their code, so the 48-hour window
-- to report a problem is not missed. The SMS body must be registered on DLT
-- exactly as written (greenroot-infra docs/sms.md).
INSERT INTO public.sms_templates (template_code, template_name, category, header_id, body, variables)
SELECT 'DELIVERY_UNCONFIRMED', 'Delivered without customer code', 'SERVICE_IMPLICIT', h.header_id,
       'GreenRoot delivery DSP-{#numeric#}-{#numeric#} was marked done by the nursery without your code. Not received? Report it in the app within 48 hours. - GreenRoot',
       '["dispatch_date", "dispatch_seq"]'
FROM (SELECT min(header_id) AS header_id FROM public.sms_headers) h
ON CONFLICT (template_code) DO NOTHING;

INSERT INTO public.email_templates (template_code, template_name, subject, body_html, body_text) VALUES
  ('DELIVERY_UNCONFIRMED', 'Delivered without customer code', 'Your GreenRoot delivery {{dispatch_code}} was marked delivered',
   $html$<p>The nursery marked delivery <strong>{{dispatch_code}}</strong> as done without your delivery code.</p>
<p>Reason given: {{reason}}</p>
<p>If you did not receive your plants, open the order in the GreenRoot app and report a problem within 48 hours.</p>$html$,
   $text$The nursery marked delivery {{dispatch_code}} as done without your delivery code.

Reason given: {{reason}}

If you did not receive your plants, open the order in the GreenRoot app and report a problem within 48 hours.$text$)
ON CONFLICT (template_code) DO NOTHING;
