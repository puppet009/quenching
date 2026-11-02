USE mall;
CREATE TABLE quenching_sandbox_payment_receipts (
 command_id VARCHAR(128) PRIMARY KEY, order_id BIGINT NOT NULL UNIQUE,
 receipt_id VARCHAR(200) NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;
