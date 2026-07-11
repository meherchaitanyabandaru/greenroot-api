ALTER TABLE public.nurseries
    DROP COLUMN IF EXISTS brand_color,
    DROP COLUMN IF EXISTS brand_icon_key,
    DROP COLUMN IF EXISTS logo_url;
