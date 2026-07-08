-- 000009_quotations_fk_cleanup.down.sql
BEGIN;

DROP INDEX IF EXISTS public.idx_quotations_recipient_mobile;
CREATE INDEX idx_quotations_recipient_mobile
    ON public.quotations (recipient_mobile)
    WHERE deleted_at IS NULL AND recipient_mobile IS NOT NULL;

-- Re-add the duplicate constraints (restores pre-009 state)
ALTER TABLE public.quotations
    ADD CONSTRAINT fk_quot_customer FOREIGN KEY (customer_user_id) REFERENCES public.users(user_id),
    ADD CONSTRAINT fk_quot_manager  FOREIGN KEY (assigned_manager_user_id) REFERENCES public.users(user_id),
    ADD CONSTRAINT fk_quot_nursery  FOREIGN KEY (nursery_id) REFERENCES public.nurseries(nursery_id),
    ADD CONSTRAINT fk_quot_order    FOREIGN KEY (converted_order_id) REFERENCES public.orders(order_id);

COMMIT;
