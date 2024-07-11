CREATE TABLE IF NOT EXISTS chat_history (
    id SERIAL PRIMARY KEY,
    chat_id BIGINT NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
