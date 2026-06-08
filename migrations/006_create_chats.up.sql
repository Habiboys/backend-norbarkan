CREATE TABLE chats (
    id         CHAR(36) PRIMARY KEY,
    room_id    CHAR(36) NOT NULL,
    user_id    CHAR(36) NOT NULL,
    message    TEXT     NOT NULL,
    type       ENUM('text','system','reaction') NOT NULL DEFAULT 'text',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_chats_room FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    CONSTRAINT fk_chats_user FOREIGN KEY (user_id) REFERENCES users(id),
    INDEX idx_chats_room_id_created (room_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
