-- Monthly plan pricing (2026-10-27). Basic / Growth / Enterprise cost
-- ₹299 / ₹499 / ₹999 a month for 5 / 10 / 20 managers; quotations, orders,
-- customers and trips are unlimited on all three. The API prices quarterly,
-- half-yearly and yearly from monthly_price (5 / 10 / 15 % off); yearly_price
-- is kept in step for reports. The free trial is one month for nurseries
-- approved from now on; trials already running keep their end date.
-- Existing subscriptions and payments are not touched.

INSERT INTO public.subscription_plans
  (plan_code, plan_name, description, monthly_price, yearly_price, max_managers, is_active, features)
VALUES
  ('BASIC', 'Basic', 'For small nurseries getting started.', 299.00, 3049.80, 5, true,
   '{"support":"community","analytics":"basic","max_managers":5,"billing_cycles":["MONTHLY","QUARTERLY","SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"unlimited_customers":true,"unlimited_trips":true,"market_posts_per_day":3}'::jsonb)
ON CONFLICT (plan_code) DO UPDATE SET
  plan_name = EXCLUDED.plan_name, description = EXCLUDED.description,
  monthly_price = EXCLUDED.monthly_price, yearly_price = EXCLUDED.yearly_price,
  max_managers = EXCLUDED.max_managers, is_active = true, features = EXCLUDED.features,
  updated_at = NOW();

UPDATE public.subscription_plans SET
  description = 'For growing nurseries with more managers and market activity.',
  monthly_price = 499.00, yearly_price = 5089.80, max_managers = 10,
  features = '{"support":"email","analytics":"full","max_managers":10,"billing_cycles":["MONTHLY","QUARTERLY","SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"unlimited_customers":true,"unlimited_trips":true,"market_posts_per_day":5}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'GROWTH';

UPDATE public.subscription_plans SET
  description = 'For large nursery operations with priority support.',
  monthly_price = 999.00, yearly_price = 10189.80, max_managers = 20,
  features = '{"support":"priority","analytics":"advanced","max_managers":20,"billing_cycles":["MONTHLY","QUARTERLY","SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"unlimited_customers":true,"unlimited_trips":true,"market_posts_per_day":null}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'ENTERPRISE';

-- The trial limits nothing either: say so, as on the paid plans.
UPDATE public.subscription_plans SET
  description = '1-month free trial for new approved nurseries.',
  features = features || '{"unlimited_customers":true,"unlimited_trips":true}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'TRIAL';
