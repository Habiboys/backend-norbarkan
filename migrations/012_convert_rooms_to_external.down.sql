ALTER TABLE rooms
    MODIFY mode ENUM('external', 'gdrive') NOT NULL DEFAULT 'gdrive';

UPDATE rooms SET mode = 'gdrive';
