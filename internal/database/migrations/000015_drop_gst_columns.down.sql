ALTER TABLE public.nurseries
    ADD COLUMN IF NOT EXISTS gst_number VARCHAR(50);

ALTER TABLE public.nursery_applications
    ADD COLUMN IF NOT EXISTS gst_number VARCHAR(50);
