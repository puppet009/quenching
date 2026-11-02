-- 种子数据：行为模拟剧本（T1-35 补充）
-- 为电商演示项目（project_id=1）预置 3 种流量模式剧本

-- 稳态流量剧本（默认）
INSERT INTO behavior_scenarios (
    project_id, name, description, scenario_type, client_type,
    base_qps, peak_qps, duration,
    user_behavior_config, traffic_pattern_config, api_endpoints,
    is_default, is_active, tenant_id
) VALUES
(
    1,
    '稳态流量-电商标准',
    '电商系统标准稳态流量模拟，覆盖浏览/搜索/详情/加购/下单/支付全链路',
    'steady',
    'web',
    20,
    50,
    3600,
    '{"browse_rate":1.0,"search_rate":0.6,"detail_rate":0.8,"cart_rate":0.5,"order_rate":0.3,"pay_rate":0.85,"min_interval":0.5,"max_interval":3.0}',
    '{"pattern":"steady","base_qps":20}',
    '[{"path":"/","method":"GET","type":"browse_home"},{"path":"/category/{cat}","method":"GET","type":"browse_category"},{"path":"/search","method":"GET","type":"search"},{"path":"/product/{id}","method":"GET","type":"view_detail"},{"path":"/cart/add","method":"POST","type":"add_cart"},{"path":"/order","method":"POST","type":"place_order"},{"path":"/pay","method":"POST","type":"pay"}]',
    true,
    true,
    0
);

-- 日周期流量剧本
INSERT INTO behavior_scenarios (
    project_id, name, description, scenario_type, client_type,
    base_qps, peak_qps, duration,
    user_behavior_config, traffic_pattern_config, api_endpoints,
    is_default, is_active, tenant_id
) VALUES
(
    1,
    '日周期流量-电商高峰',
    '模拟真实日周期流量波动，午间和晚间为高峰',
    'daily_pattern',
    'web',
    30,
    80,
    86400,
    '{"browse_rate":1.0,"search_rate":0.65,"detail_rate":0.75,"cart_rate":0.45,"order_rate":0.28,"pay_rate":0.8}',
    '{"pattern":"daily","period_hours":24,"peak_hours":[12,20],"valley_hours":[4]}',
    '[{"path":"/","method":"GET","type":"browse_home"},{"path":"/product/{id}","method":"GET","type":"view_detail"},{"path":"/order","method":"POST","type":"place_order"}]',
    false,
    true,
    0
);

-- 尖刺流量剧本
INSERT INTO behavior_scenarios (
    project_id, name, description, scenario_type, client_type,
    base_qps, peak_qps, duration,
    user_behavior_config, traffic_pattern_config, api_endpoints,
    is_default, is_active, tenant_id
) VALUES
(
    1,
    '尖刺流量-秒杀场景',
    '模拟秒杀活动的突发流量尖刺，2分钟内QPS飙升3倍',
    'spike',
    'web',
    30,
    100,
    1800,
    '{"browse_rate":1.0,"search_rate":0.5,"detail_rate":0.9,"cart_rate":0.8,"order_rate":0.7,"pay_rate":0.9}',
    '{"pattern":"spike","spike_at_seconds":600,"spike_width_seconds":120,"spike_multiplier":3.0}',
    '[{"path":"/product/{id}","method":"GET","type":"view_detail"},{"path":"/order","method":"POST","type":"place_order"},{"path":"/pay","method":"POST","type":"pay"}]',
    false,
    true,
    0
);
