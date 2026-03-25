CREATE TABLE IF NOT EXISTS issues (
  id            INT AUTO_INCREMENT PRIMARY KEY,
  issue_type    VARCHAR(50)  NOT NULL DEFAULT 'task',
                             -- task | epic | bug | refactor
  status        VARCHAR(30)  NOT NULL DEFAULT 'draft',
                             -- draft | open | in_progress | in_review
                             -- reviewed | repairing | closed
  title         TEXT         NOT NULL,
  description   TEXT,
  specialty     VARCHAR(50),
  branch        VARCHAR(255),
  pr_number     INT,
  assignee      VARCHAR(100),
  parent_id     INT,
  created_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  closed_at     TIMESTAMP    NULL,
  FOREIGN KEY (parent_id) REFERENCES issues(id)
);
