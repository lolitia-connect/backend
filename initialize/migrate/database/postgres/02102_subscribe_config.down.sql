-- Reverses the subscribe User-Agent settings the up migration seeds.
DELETE FROM "system"
WHERE "category" = 'subscribe'
  AND "key" IN ('UserAgentLimit', 'UserAgentList');
