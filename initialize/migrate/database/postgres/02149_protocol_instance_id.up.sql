ALTER TABLE "nodes" ADD COLUMN IF NOT EXISTS "protocol_id" VARCHAR(100) NOT NULL DEFAULT '';

UPDATE "nodes" SET "protocol_id" = '1' WHERE "protocol_id" = '';

UPDATE "servers" s
SET "protocols" = migrated."protocols"::text
FROM (
	SELECT id,
		   jsonb_agg(
			   CASE
				   WHEN COALESCE(protocol ->> 'id', '') = ''
					   THEN jsonb_set(protocol, '{id}', to_jsonb(protocol_index::text), true)
				   ELSE protocol
			   END
			   ORDER BY ord
		   ) AS "protocols"
	FROM (
		SELECT s.id,
			   protocol_rows.protocol,
			   protocol_rows.ord,
			   row_number() OVER (
				   PARTITION BY s.id, protocol_rows.protocol ->> 'type'
				   ORDER BY protocol_rows.ord
			   ) AS protocol_index
		FROM "servers" s
		CROSS JOIN LATERAL jsonb_array_elements(s."protocols"::jsonb) WITH ORDINALITY AS protocol_rows(protocol, ord)
		WHERE s."protocols" IS NOT NULL
		  AND s."protocols" <> ''
		  AND left(trim(s."protocols"), 1) = '['
	) protocol_indexed
	GROUP BY id
) migrated
WHERE migrated.id = s.id;
