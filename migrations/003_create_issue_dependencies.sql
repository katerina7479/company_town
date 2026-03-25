CREATE TABLE IF NOT EXISTS issue_dependencies (
  issue_id      INT NOT NULL,
  depends_on_id INT NOT NULL,
  PRIMARY KEY (issue_id, depends_on_id),
  FOREIGN KEY (issue_id) REFERENCES issues(id),
  FOREIGN KEY (depends_on_id) REFERENCES issues(id)
);
