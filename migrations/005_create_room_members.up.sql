CREATE TABLE room_members (
    id        CHAR(36) PRIMARY KEY,
    room_id   CHAR(36) NOT NULL,
    user_id   CHAR(36) NOT NULL,
    role      ENUM('host', 'member') NOT NULL DEFAULT 'member',
    is_ready  BOOLEAN  NOT NULL DEFAULT FALSE,
    joined_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    left_at   DATETIME,
    CONSTRAINT fk_room_members_room FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    CONSTRAINT fk_room_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE KEY uq_room_user (room_id, user_id),
    INDEX idx_room_members_room_id (room_id),
    INDEX idx_room_members_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
