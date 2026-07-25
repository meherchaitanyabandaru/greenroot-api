-- Order PDF verification (QR) and document storage/versioning history.
-- Mirrors quotation_verifications / quotation_documents exactly.

CREATE TABLE public.order_verifications (
    verification_id bigserial PRIMARY KEY,
    order_id bigint NOT NULL,
    token character varying(128) NOT NULL UNIQUE,
    status character varying(20) NOT NULL DEFAULT 'ACTIVE',
    created_at timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at timestamp without time zone,
    revoked_by bigint,
    CONSTRAINT order_verifications_status_check CHECK (status IN ('ACTIVE', 'REVOKED')),
    CONSTRAINT order_verifications_order_id_fkey FOREIGN KEY (order_id) REFERENCES public.orders(order_id) ON DELETE CASCADE,
    CONSTRAINT order_verifications_revoked_by_fkey FOREIGN KEY (revoked_by) REFERENCES public.users(user_id)
);

CREATE UNIQUE INDEX idx_order_verifications_active ON public.order_verifications USING btree (order_id) WHERE (status = 'ACTIVE');
CREATE INDEX idx_order_verifications_token ON public.order_verifications USING btree (token);

CREATE TABLE public.order_documents (
    doc_id bigserial PRIMARY KEY,
    order_id bigint NOT NULL,
    version integer NOT NULL DEFAULT 1,
    object_key text NOT NULL,
    sha256_hash character varying(64) NOT NULL,
    mime_type character varying(100) NOT NULL DEFAULT 'application/pdf',
    file_size bigint NOT NULL DEFAULT 0,
    generated_by bigint,
    generated_by_name character varying(255),
    is_current boolean NOT NULL DEFAULT true,
    created_at timestamp without time zone NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT order_documents_version_positive CHECK (version > 0),
    CONSTRAINT order_documents_file_size_nonnegative CHECK (file_size >= 0),
    CONSTRAINT order_documents_unique_version UNIQUE (order_id, version),
    CONSTRAINT order_documents_order_id_fkey FOREIGN KEY (order_id) REFERENCES public.orders(order_id) ON DELETE CASCADE,
    CONSTRAINT order_documents_generated_by_fkey FOREIGN KEY (generated_by) REFERENCES public.users(user_id)
);

CREATE UNIQUE INDEX idx_order_documents_current ON public.order_documents USING btree (order_id) WHERE (is_current = true);
