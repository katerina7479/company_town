CREATE TABLE IF NOT EXISTS quality_metrics (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  check_name VARCHAR(100) NOT NULL,
  status     VARCHAR(20)  NOT NULL,
                           -- pass | fail | warn | error
  output     TEXT,
  value      DOUBLE,       -- set for metric checks, NULL for pass/fail checks
  run_at     TIMESTAMP    NOT NULL,
  error      TEXT          -- set when the check could not be executed
);
