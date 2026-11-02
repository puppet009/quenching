// 业务领域静态树（来源：产品规划文档「四、业务领域分类」）。
// 两级结构：大领域（5）→ 细分领域（42，文档口径"45+"）。
// 前端写死作为「内容地图」；每个细分领域的项目数由模板列表（/templates）
// 按 domain code 实时统计；固定分类始终可浏览，项目数量由实际模板决定。
// template.yaml 的 domain 字段必须填这里的细分 code。

/** 细分领域 */
export interface DomainNode {
  /** 英文 code，与 template.yaml 的 domain 对齐 */
  code: string;
  /** 中文显示名 */
  label: string;
  /** 典型项目举例 */
  examples: string[];
}

/** 大领域 */
export interface DomainGroup {
  code: string;
  label: string;
  /** 封面渐变（Tailwind class，对应品牌色） */
  gradient: string;
  children: DomainNode[];
}

export const DOMAIN_TREE: DomainGroup[] = [
  {
    code: "consumer",
    label: "互联网消费",
    gradient: "from-brand-500 to-brand-400",
    children: [
      { code: "ecommerce", label: "电商交易", examples: ["综合商城", "秒杀系统", "拼团", "直播电商", "二手闲置"] },
      { code: "social", label: "社交社区", examples: ["论坛", "朋友圈/动态", "问答社区", "兴趣社群"] },
      { code: "im", label: "即时通讯", examples: ["单聊IM", "群聊系统", "实时音视频", "直播弹幕"] },
      { code: "content", label: "内容平台", examples: ["短视频", "长视频", "资讯推荐", "小说阅读", "播客"] },
      { code: "online-edu", label: "在线教育", examples: ["直播课堂", "录播课程", "在线考试", "题库系统"] },
      { code: "local-life", label: "本地生活", examples: ["外卖平台", "团购到店", "家政服务", "上门服务"] },
      { code: "travel", label: "出行旅游", examples: ["网约车", "酒店预订", "机票火车票", "旅游攻略"] },
      { code: "game", label: "游戏娱乐", examples: ["手游服务端", "对战匹配", "排行榜", "公会系统"] },
      { code: "tools", label: "工具应用", examples: ["云笔记", "网盘存储", "在线设计", "效率工具"] },
      { code: "marketing", label: "营销活动", examples: ["抽奖系统", "签到打卡", "优惠券", "邀请裂变"] },
    ],
  },
  {
    code: "finance",
    label: "金融与支付",
    gradient: "from-ai to-brand-400",
    children: [
      { code: "payment", label: "支付清算", examples: ["第三方支付", "对账系统", "清结算", "收银台"] },
      { code: "banking", label: "银行核心", examples: ["账户系统", "转账汇款", "信贷系统", "核心账务"] },
      { code: "securities", label: "证券基金", examples: ["行情系统", "交易撮合", "持仓管理", "基金申赎"] },
      { code: "insurance", label: "保险业务", examples: ["保单系统", "理赔核保", "续保系统", "再保险"] },
      { code: "consumer-finance", label: "消费金融", examples: ["授信审批", "风控系统", "还款催收", "额度管理"] },
      { code: "web3", label: "区块链/Web3", examples: ["钱包", "交易所", "DApp", "智能合约"] },
    ],
  },
  {
    code: "enterprise",
    label: "企业与政务",
    gradient: "from-ok to-brand-400",
    children: [
      { code: "oa", label: "协同办公", examples: ["OA系统", "审批流", "文档管理", "会议系统"] },
      { code: "crm", label: "客户关系", examples: ["CRM", "销售管理", "客服系统", "工单系统"] },
      { code: "hrm", label: "人力资源", examples: ["e-HR", "考勤薪酬", "招聘系统", "绩效系统"] },
      { code: "finance-system", label: "财务系统", examples: ["报销系统", "记账系统", "财务报表", "发票管理"] },
      { code: "erp", label: "ERP制造", examples: ["生产计划", "仓储WMS", "供应链", "MES系统"] },
      { code: "project-mgmt", label: "项目管理", examples: ["项目协作", "任务管理", "甘特图", "工时系统"] },
      { code: "gov", label: "政务服务", examples: ["政务门户", "一网通办", "审批系统", "12345热线"] },
      { code: "healthcare", label: "医疗健康", examples: ["HIS医院系统", "预约挂号", "电子病历", "医保对接"] },
      { code: "edu-mgmt", label: "教育管理", examples: ["教务系统", "学籍管理", "选课系统", "校园一卡通"] },
      { code: "legal", label: "法务合同", examples: ["合同管理", "电子签章", "合规审查", "知识库"] },
    ],
  },
  {
    code: "industry",
    label: "物流与产业",
    gradient: "from-warn to-brand-400",
    children: [
      { code: "logistics", label: "快递物流", examples: ["订单跟踪", "仓储WMS", "运输TMS", "分拣系统"] },
      { code: "industrial-internet", label: "工业互联网", examples: ["设备监控", "数据采集", "SCADA", "工业APP"] },
      { code: "energy", label: "能源电力", examples: ["电网监控", "新能源管理", "用电计量", "配电系统"] },
      { code: "smart-agri", label: "智慧农业", examples: ["环境监测", "智能灌溉", "农机调度", "溯源系统"] },
      { code: "smart-traffic", label: "智能交通", examples: ["信号控制", "违章识别", "路况监控", "ETC系统"] },
      { code: "iov", label: "车联网/智能汽车", examples: ["TSP平台", "车机互联", "OTA升级", "车队管理"] },
      { code: "smart-home", label: "智能家居", examples: ["IoT平台", "设备控制", "场景联动", "语音助手"] },
    ],
  },
  {
    code: "platform",
    label: "技术平台与基础设施",
    gradient: "from-danger to-brand-400",
    children: [
      { code: "bigdata", label: "大数据平台", examples: ["数据仓库", "实时计算", "BI报表", "数据同步"] },
      { code: "ai-ml", label: "AI/推荐系统", examples: ["推荐引擎", "搜索服务", "模型服务", "特征平台"] },
      { code: "aigc", label: "AIGC应用", examples: ["大模型应用", "Agent平台", "向量数据库", "Prompt工程"] },
      { code: "cloud-native", label: "云原生平台", examples: ["容器平台", "微服务网关", "服务治理", "配置中心"] },
      { code: "iot-platform", label: "物联网平台", examples: ["设备接入", "消息总线", "规则引擎", "设备影子"] },
      { code: "data-midplatform", label: "数据中台", examples: ["数据治理", "主数据", "数据服务", "元数据管理"] },
      { code: "ads", label: "广告营销", examples: ["广告投放", "DMP人群", "实时竞价", "归因分析"] },
      { code: "cms", label: "内容管理", examples: ["CMS系统", "媒体资源管理", "发布引擎", "全站搜索"] },
      { code: "api-gateway", label: "API网关/开放平台", examples: ["API网关", "开发者平台", "鉴权中心", "限流熔断"] },
    ],
  },
];

/** 全部细分节点（拍平，按树顺序） */
export const ALL_DOMAINS: DomainNode[] = DOMAIN_TREE.flatMap((g) => g.children);

/** 按 code 查细分节点 */
export function findDomain(code: string): DomainNode | undefined {
  return ALL_DOMAINS.find((d) => d.code === code);
}

/** 细分 code → 中文名，未知 code 原样返回 */
export function domainLabel(code: string): string {
  return findDomain(code)?.label ?? code;
}

/** 细分 code → 所属大领域 */
export function domainGroupOf(code: string): DomainGroup | undefined {
  return DOMAIN_TREE.find((g) => g.children.some((d) => d.code === code));
}
