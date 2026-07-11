ALTER TABLE public.nursery_applications
    DROP COLUMN IF EXISTS gst_number;

ALTER TABLE public.nurseries
    DROP COLUMN IF EXISTS gst_number;
