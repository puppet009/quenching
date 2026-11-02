FROM mysql:8.0.40
COPY upstream/mall/document/sql/mall.sql /docker-entrypoint-initdb.d/10-mall.sql
COPY adaptation/20-reset-customers.sql /docker-entrypoint-initdb.d/20-reset-customers.sql

COPY adaptation/30-sandbox-payments.sql /docker-entrypoint-initdb.d/30-sandbox-payments.sql
