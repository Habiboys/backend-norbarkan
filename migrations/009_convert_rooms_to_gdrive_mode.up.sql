ALTER TABLE rooms
    MODIFY mode ENUM('local', 'external', 'gdrive') NOT NULL DEFAULT 'gdrive';

UPDATE rooms
SET mode = 'gdrive';

ALTER TABLE rooms
    MODIFY mode ENUM('gdrive') NOT NULL DEFAULT 'gdrive';
