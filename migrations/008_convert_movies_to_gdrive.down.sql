DROP INDEX idx_movies_drive_file_id ON movies;

ALTER TABLE movies
    MODIFY source_type ENUM('local', 'external') NOT NULL;

UPDATE movies
SET
    source_type = 'external',
    external_url = COALESCE(external_url, drive_url),
    provider_name = COALESCE(provider_name, 'Google Drive')
WHERE deleted_at IS NULL;

ALTER TABLE movies
    DROP COLUMN drive_url,
    DROP COLUMN drive_file_id;
