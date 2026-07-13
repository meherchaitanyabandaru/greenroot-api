UPDATE public.quotations
SET status = 'CUSTOMER_REJECTED'
WHERE status = 'EXPIRED';

ALTER TABLE public.quotations DROP CONSTRAINT IF EXISTS chk_quotation_status;

ALTER TABLE public.quotations ADD CONSTRAINT chk_quotation_status
CHECK (
    status IN (
        'INTERNAL_DRAFT',
        'CUSTOMER_DRAFT',
        'CUSTOMER_SENT',
        'CUSTOMER_ACCEPTED',
        'CUSTOMER_REJECTED',
        'CONVERTED'
    )
);
