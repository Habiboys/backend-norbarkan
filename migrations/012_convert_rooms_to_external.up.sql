ALTER TABLE rooms
    MODIFY mode ENUM('gdrive', 'external') NOT NULL DEFAULT 'external';

UPDATE rooms SET mode = 'external';

ALTER TABLE rooms
    MODIFY mode ENUM('external') NOT NULL DEFAULT 'external';
