ALTER TABLE movies
    ADD COLUMN drive_file_id VARCHAR(255) NULL AFTER external_url,
    ADD COLUMN drive_url VARCHAR(1000) NULL AFTER drive_file_id;

ALTER TABLE movies
    MODIFY source_type ENUM('local', 'external', 'gdrive') NOT NULL DEFAULT 'gdrive';

UPDATE movies
SET
    drive_url = COALESCE(drive_url, external_url),
    source_type = 'gdrive',
    provider_name = 'Google Drive',
    transcode_status = 'done'
;

ALTER TABLE movies
    MODIFY source_type ENUM('gdrive') NOT NULL DEFAULT 'gdrive';

CREATE INDEX idx_movies_drive_file_id ON movies(drive_file_id);
