CREATE TABLE IF NOT EXISTS agents (
  id            INT AUTO_INCREMENT PRIMARY KEY,
  name          VARCHAR(100) NOT NULL UNIQUE,
  type          VARCHAR(50)  NOT NULL,
                             -- mayor | architect | conductor
                             -- prole | reviewer | janitor | artisan
  specialty     VARCHAR(50),
  status        VARCHAR(20)  NOT NULL DEFAULT 'idle',
                             -- working | idle | dead
  current_issue INT,
  tmux_session  VARCHAR(100),
  worktree_path TEXT,
  time_created  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  time_ended    TIMESTAMP    NULL,
  FOREIGN KEY (current_issue) REFERENCES issues(id)
);
