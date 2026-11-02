-- Preserve merchandise and configuration, but remove upstream demo customers.
-- Runs only on a fresh sandbox database after the upstream schema import.
USE mall;
SET FOREIGN_KEY_CHECKS = 0;
TRUNCATE TABLE oms_order_operate_history;
TRUNCATE TABLE oms_order_return_apply;
TRUNCATE TABLE oms_order_item;
TRUNCATE TABLE oms_order;
TRUNCATE TABLE oms_cart_item;
TRUNCATE TABLE sms_coupon_history;
TRUNCATE TABLE ums_member_receive_address;
TRUNCATE TABLE ums_member_statistics_info;
TRUNCATE TABLE ums_integration_change_history;
TRUNCATE TABLE ums_growth_change_history;
TRUNCATE TABLE ums_member_login_log;
TRUNCATE TABLE ums_member_member_tag_relation;
TRUNCATE TABLE ums_member_product_category_relation;
TRUNCATE TABLE ums_member;
UPDATE pms_sku_stock SET lock_stock = 0;
SET FOREIGN_KEY_CHECKS = 1;
