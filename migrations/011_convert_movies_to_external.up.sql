ALTER TABLE movies
    MODIFY source_type ENUM('gdrive', 'external') NOT NULL DEFAULT 'external';

UPDATE movies SET source_type = 'external' WHERE source_type = 'gdrive';

-- Drop Drive-specific columns once data is migrated
ALTER TABLE movies
    DROP COLUMN drive_file_id,
    DROP COLUMN drive_url;

ALTER TABLE movies
    MODIFY source_type ENUM('external') NOT NULL DEFAULT 'external';
