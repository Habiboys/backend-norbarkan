CREATE TABLE room_events (
    id         CHAR(36) PRIMARY KEY,
    room_id    CHAR(36) NOT NULL,
    user_id    CHAR(36) NOT NULL,
    event_type ENUM('play','pause','seek','join','leave','end') NOT NULL,
    payload    JSON,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_room_events_room FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE,
    CONSTRAINT fk_room_events_user FOREIGN KEY (user_id) REFERENCES users(id),
    INDEX idx_room_events_room_id (room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
