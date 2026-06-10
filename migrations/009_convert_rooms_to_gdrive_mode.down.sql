ALTER TABLE rooms
    MODIFY mode ENUM('local', 'external', 'gdrive') NOT NULL DEFAULT 'external';

UPDATE rooms
SET mode = 'external'
WHERE mode = 'gdrive';

ALTER TABLE rooms
    MODIFY mode ENUM('local', 'external') NOT NULL DEFAULT 'external';
