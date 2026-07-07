ALTER TABLE movies
    MODIFY source_type ENUM('external', 'gdrive') NOT NULL DEFAULT 'gdrive';

ALTER TABLE movies
    ADD COLUMN drive_file_id VARCHAR(255) NULL AFTER external_url,
    ADD COLUMN drive_url VARCHAR(1000) NULL AFTER drive_file_id;

UPDATE movies SET source_type = 'gdrive';
