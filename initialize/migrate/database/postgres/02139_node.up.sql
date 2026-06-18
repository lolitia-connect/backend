-- Only add the columns to "servers" when they do not already exist

-- Add longitude
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'servers' AND column_name = 'longitude') THEN
        ALTER TABLE "servers" ADD COLUMN "longitude" VARCHAR(255) DEFAULT '';
    END IF;
END $$;

-- Add latitude
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'servers' AND column_name = 'latitude') THEN
        ALTER TABLE "servers" ADD COLUMN "latitude" VARCHAR(255) DEFAULT '';
    END IF;
END $$;

-- Add longitude_center
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'servers' AND column_name = 'longitude_center') THEN
        ALTER TABLE "servers" ADD COLUMN "longitude_center" VARCHAR(255) DEFAULT '';
    END IF;
END $$;

-- Add latitude_center
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'servers' AND column_name = 'latitude_center') THEN
        ALTER TABLE "servers" ADD COLUMN "latitude_center" VARCHAR(255) DEFAULT '';
    END IF;
END $$;
