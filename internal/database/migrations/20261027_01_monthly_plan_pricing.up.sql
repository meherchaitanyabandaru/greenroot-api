-- Monthly plan pricing (2026-10-27). Silver / Gold / Platinum (codes BASIC /
-- GROWTH / ENTERPRISE, kept so payments, promos and links stay valid) cost
-- ₹299 / ₹499 / ₹999 a month for 1 / 5 / 10 managers. Every plan has
-- unlimited orders and quotations and can post local market ads. The API
-- prices quarterly, half-yearly and yearly from monthly_price (5 / 10 / 15 %
-- off); yearly_price is kept in step for reports. The free trial is one month
-- for nurseries approved from now on; trials already running keep their end
-- date. Existing subscriptions and payments are not touched.

INSERT INTO public.subscription_plans
  (plan_code, plan_name, description, monthly_price, yearly_price, max_managers, is_active, features)
VALUES
  ('BASIC', 'Silver', 'For small nurseries run by the owner and one manager.', 299.00, 3049.80, 1, true,
   '{"tier":"SILVER","support":"community","analytics":"basic","max_managers":1,"billing_cycles":["MONTHLY","QUARTERLY","SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"local_market_ads":true,"market_posts_per_day":3}'::jsonb)
ON CONFLICT (plan_code) DO UPDATE SET
  plan_name = EXCLUDED.plan_name, description = EXCLUDED.description,
  monthly_price = EXCLUDED.monthly_price, yearly_price = EXCLUDED.yearly_price,
  max_managers = EXCLUDED.max_managers, is_active = true, features = EXCLUDED.features,
  updated_at = NOW();

UPDATE public.subscription_plans SET
  plan_name = 'Gold',
  description = 'For growing nurseries with a team of managers.',
  monthly_price = 499.00, yearly_price = 5089.80, max_managers = 5,
  features = '{"tier":"GOLD","support":"email","analytics":"full","max_managers":5,"billing_cycles":["MONTHLY","QUARTERLY","SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"local_market_ads":true,"market_posts_per_day":5}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'GROWTH';

UPDATE public.subscription_plans SET
  plan_name = 'Platinum',
  description = 'For large nursery operations, with priority support.',
  monthly_price = 999.00, yearly_price = 10189.80, max_managers = 10,
  features = '{"tier":"PLATINUM","support":"priority","analytics":"advanced","max_managers":10,"billing_cycles":["MONTHLY","QUARTERLY","SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"local_market_ads":true,"market_posts_per_day":null}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'ENTERPRISE';

UPDATE public.subscription_plans SET
  description = '1-month free trial for new approved nurseries.',
  features = features || '{"local_market_ads":true}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'TRIAL';
