-- Keep the admin fleet view aligned with driver onboarding.
-- Existing drivers historically stored vehicle details on public.drivers only.

WITH driver_vehicle AS (
  SELECT DISTINCT ON (UPPER(TRIM(d.vehicle_number)))
    UPPER(TRIM(d.vehicle_number)) AS vehicle_number,
    NULLIF(TRIM(d.vehicle_type), '') AS vehicle_type,
    NULLIF(TRIM(CONCAT_WS(' ', u.first_name, u.last_name)), '') AS owner_name,
    NULLIF(TRIM(u.mobile), '') AS mobile,
    CASE
      WHEN COALESCE(d.status::text, '') = 'ACTIVE'
        AND COALESCE(d.approval_status, '') = 'APPROVED'
      THEN 'ACTIVE'
      ELSE 'INACTIVE'
    END AS status
  FROM public.drivers d
  LEFT JOIN public.users u ON u.user_id = d.user_id
  WHERE COALESCE(d.status::text, '') <> 'DELETED'
    AND NULLIF(TRIM(d.vehicle_number), '') IS NOT NULL
  ORDER BY UPPER(TRIM(d.vehicle_number)),
    CASE WHEN COALESCE(d.approval_status, '') = 'APPROVED' THEN 0 ELSE 1 END,
    d.updated_at DESC NULLS LAST,
    d.driver_id DESC
)
INSERT INTO public.vehicles (
  vehicle_number, vehicle_type, owner_name, mobile, status, created_at, updated_at
)
SELECT vehicle_number, vehicle_type, owner_name, mobile, status, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM driver_vehicle
ON CONFLICT (vehicle_number) DO UPDATE
SET vehicle_type = COALESCE(EXCLUDED.vehicle_type, public.vehicles.vehicle_type),
  owner_name = COALESCE(EXCLUDED.owner_name, public.vehicles.owner_name),
  mobile = COALESCE(EXCLUDED.mobile, public.vehicles.mobile),
  status = EXCLUDED.status,
  updated_at = CURRENT_TIMESTAMP;
