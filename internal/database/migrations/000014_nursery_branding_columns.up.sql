ALTER TABLE public.nurseries
    ADD COLUMN IF NOT EXISTS logo_url TEXT,
    ADD COLUMN IF NOT EXISTS brand_icon_key VARCHAR(64),
    ADD COLUMN IF NOT EXISTS brand_color VARCHAR(16);
