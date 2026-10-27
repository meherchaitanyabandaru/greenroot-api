-- Back to six-month / yearly pricing. BASIC is hidden, not deleted, because
-- subscriptions may point at it.
UPDATE public.subscription_plans SET is_active = false, updated_at = NOW() WHERE plan_code = 'BASIC';

UPDATE public.subscription_plans SET
  monthly_price = 2499.00, yearly_price = 3999.00, max_managers = 5,
  features = '{"support":"email","analytics":"full","max_managers":5,"billing_cycles":["SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"market_posts_per_day":5,"mrp_six_month":4999,"mrp_yearly":19999}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'GROWTH';

UPDATE public.subscription_plans SET
  monthly_price = 7499.00, yearly_price = 11999.00, max_managers = NULL,
  features = '{"support":"priority","analytics":"advanced","max_managers":null,"billing_cycles":["SIX_MONTH","YEARLY"],"unlimited_orders":true,"unlimited_quotations":true,"market_posts_per_day":null,"mrp_six_month":14999,"mrp_yearly":59999}'::jsonb,
  updated_at = NOW()
WHERE plan_code = 'ENTERPRISE';

UPDATE public.subscription_plans SET
  description = '6-month free trial for new approved nurseries.',
  features = features - 'unlimited_customers' - 'unlimited_trips', updated_at = NOW()
WHERE plan_code = 'TRIAL';
