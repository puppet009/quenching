import { useMemo } from "react";
import { Link, useParams } from "react-router-dom";
import { siteInfo } from "../config/site";

/**
 * 法律条款页（服务协议 / 隐私政策）
 * 路由：/legal/terms、/legal/privacy
 * 内容为 MVP 阶段的标准框架文本，正式上线前请由法务审核定稿。
 */

type LegalKind = "terms" | "privacy";

interface LegalDoc {
  title: string;
  updatedAt: string;
  intro: string;
  sections: { heading: string; body: string[] }[];
}

const company = siteInfo.companyName || "淬火平台运营方";

const DOCS: Record<LegalKind, LegalDoc> = {
  terms: {
    title: "服务协议",
    updatedAt: "2026-09-22",
    intro:
      "请在使用淬火 Quenching（以下简称“本平台”）前仔细阅读本协议。注册、登录或使用本平台服务，即表示您已充分理解并同意接受本协议全部条款。",
    sections: [
      {
        heading: "一、协议范围与接受",
        body: [
          `本协议由您与${company}共同缔结，具有合同效力。本协议内容包括协议正文及本平台已发布或将来可能发布的各类规则，规则均为本协议不可分割的组成部分。`,
          "您在使用本平台提供的各项服务前，应当认真阅读并同意本协议；如不同意，应停止注册和使用。",
        ],
      },
      {
        heading: "二、账号注册与安全",
        body: [
          "您应提供真实、准确、完整的注册信息，并在信息变更时及时更新。您需对账号下的全部行为承担法律责任。",
          "请妥善保管账号与密码，因您主动泄露、保管不善或遭受他人攻击、诈骗所导致的损失，由您自行承担；如发现账号被盗用，应立即通知本平台。",
        ],
      },
      {
        heading: "三、服务内容",
        body: [
          "本平台提供基于隔离沙箱的生产故障仿真训练、人机协作练习、能力评估及相关内容。沙箱环境仅供学习演练，不得用于任何非法用途或攻击平台以外的系统。",
          "本平台有权根据产品运营情况对服务内容进行调整、中断或部分终止，并将尽合理努力提前通知。",
        ],
      },
      {
        heading: "四、用户行为规范",
        body: [
          "您不得利用本平台从事危害网络安全、侵犯他人合法权益、违反法律法规或公序良俗的活动，包括但不限于攻击真实生产系统、传播恶意代码、干扰平台正常运行等。",
          "如违反上述规范，本平台有权视情节采取警告、限制功能、暂停或终止服务等措施，并保留追究法律责任的权利。",
        ],
      },
      {
        heading: "五、知识产权",
        body: [
          "本平台所包含的软件、课程内容、文字、图表、标识及训练场景设计等，其知识产权均归本平台或相应权利人所有，未经书面许可不得复制、转载或商用。",
          "您在训练过程中提交的内容，您保有相应权利；同时授予本平台在提供服务所必需的范围内进行存储、处理与展示的权利。",
        ],
      },
      {
        heading: "六、免责声明",
        body: [
          "本平台以“现状”提供服务。沙箱为模拟环境，平台不对训练结果用于真实生产环境所产生的后果承担责任，实际运维请以专业判断为准。",
          "因不可抗力、网络故障、系统维护等非本平台故意或重大过失原因导致的服务中断或数据损失，本平台不承担赔偿责任。",
        ],
      },
      {
        heading: "七、协议变更与终止",
        body: [
          "本平台可根据法律法规及业务需要修订本协议，修订后的协议将在页面公布。若您在协议更新后继续使用服务，即视为接受修订内容。",
          "您可随时停止使用并注销账号；本平台亦可在您严重违约时终止提供服务。",
        ],
      },
      {
        heading: "八、法律适用与争议解决",
        body: [
          "本协议的订立、执行与解释均适用中华人民共和国法律。",
          "因本协议或服务产生的争议，双方应友好协商解决；协商不成的，任一方可向本平台运营方所在地有管辖权的人民法院提起诉讼。",
        ],
      },
      {
        heading: "九、联系方式",
        body: [`如对本协议有任何疑问，可通过邮箱 ${siteInfo.contactEmail} 与我们联系。`],
      },
    ],
  },

  privacy: {
    title: "隐私政策",
    updatedAt: "2026-09-22",
    intro:
      "本政策说明淬火 Quenching 如何收集、使用、存储和保护您的个人信息。我们深知个人信息的重要性，将严格依照《中华人民共和国个人信息保护法》等法律法规采取相应保护措施。",
    sections: [
      {
        heading: "一、我们收集的信息",
        body: [
          "注册与登录信息：手机号或邮箱、昵称、密码（加密存储）及身份验证信息。",
          "训练过程信息：您启动的沙箱、任务完成情况、操作日志、能力评分等，用于生成训练记录与评估报告。",
          "设备与日志信息：IP 地址、浏览器类型、访问时间、页面访问记录等，用于安全防护与服务改进。",
        ],
      },
      {
        heading: "二、信息的使用",
        body: [
          "我们将收集的信息用于：提供沙箱训练与账号服务；进行身份验证与安全风控；评估和展示您的训练成果；改进产品功能与体验；履行法律法规要求的义务。",
          "未经您同意，我们不会将上述信息用于本政策未载明的其他用途。",
        ],
      },
      {
        heading: "三、信息的共享、转让与公开",
        body: [
          "我们不会向第三方出售您的个人信息。仅在以下情形下共享：取得您的明确同意；为完成必要的基础设施服务而委托受保密义务约束的服务供应商；依据法律法规或行政、司法机关的强制性要求。",
          "我们要求第三方在授权范围内处理信息，并采取不低于本政策的保护措施。",
        ],
      },
      {
        heading: "四、Cookie 与同类技术",
        body: [
          "我们使用 Cookie 及本地存储来维持登录状态、记住您的偏好设置。您可通过浏览器设置管理或清除 Cookie，但可能影响部分功能的正常使用。",
        ],
      },
      {
        heading: "五、信息的存储与保护",
        body: [
          "您的个人信息存储于中华人民共和国境内，存储期限为实现处理目的所必需的最短期限。",
          "我们采取访问控制、传输与存储加密、密码哈希、网络隔离等技术措施保护信息安全，并建立了相应的安全事件应急预案。",
        ],
      },
      {
        heading: "六、您的权利",
        body: [
          "您有权查询、复制、更正、补充您的个人信息，有权注销账号、删除或撤回同意，也可要求对自动化决策结果进行说明。",
          "如需行使上述权利，可通过本政策末尾的联系方式提出，我们将在法定期限内予以处理。",
        ],
      },
      {
        heading: "七、未成年人信息保护",
        body: [
          "本平台主要面向接受过工程技术教育或在职的成年用户。若您为未满十四周岁的未成年人，应在监护人同意和指导下使用；如发现我们在未获监护同意的情况下收集了儿童信息，将尽快删除。",
        ],
      },
      {
        heading: "八、政策更新",
        body: [
          "我们可能适时修订本政策，重大变更将通过页面公告等显著方式通知。更新后的政策自公布之日起生效。",
        ],
      },
      {
        heading: "九、联系我们",
        body: [`如对本政策或个人信息保护有任何疑问、意见或投诉，可通过邮箱 ${siteInfo.contactEmail} 与我们联系。`],
      },
    ],
  },
};

export function LegalPage() {
  const { kind } = useParams<{ kind: string }>();
  const doc = useMemo(() => DOCS[(kind as LegalKind)] ?? null, [kind]);

  if (!doc) {
    return (
      <div className="page-container py-24 text-center">
        <p className="text-ink-500">未找到该文档。</p>
        <Link to="/" className="mt-4 inline-block text-brand-600 font-medium">
          返回首页
        </Link>
      </div>
    );
  }

  return (
    <div className="bg-white">
      {/* 文档头部 */}
      <div className="border-b border-ink-100 bg-ink-50/60">
        <div className="page-container py-10">
          <nav className="flex items-center gap-2 text-[13px] text-ink-400 mb-4">
            <Link to="/" className="hover:text-ink-600">首页</Link>
            <span>/</span>
            <span className="text-ink-600">{doc.title}</span>
          </nav>
          <h1 className="text-3xl md:text-4xl font-bold text-ink-900">{doc.title}</h1>
          <p className="mt-3 text-[13px] text-ink-400">最后更新：{doc.updatedAt}</p>
        </div>
      </div>

      {/* 正文 */}
      <div className="page-container py-12 md:py-16">
        <div className="max-w-3xl">
          <p className="text-[15px] text-ink-600 leading-8 p-5 rounded-xl bg-brand-50/60 border border-brand-100">
            {doc.intro}
          </p>

          <div className="mt-10 space-y-10">
            {doc.sections.map((s, i) => (
              <section key={i}>
                <h2 className="text-lg md:text-xl font-bold text-ink-900 mb-3">
                  {s.heading}
                </h2>
                {s.body.map((p, j) => (
                  <p key={j} className="text-[14.5px] text-ink-600 leading-8 mb-2">
                    {p}
                  </p>
                ))}
              </section>
            ))}
          </div>

          {/* 文档切换 */}
          <div className="mt-14 pt-8 border-t border-ink-100 flex flex-wrap gap-x-6 gap-y-2 text-[13px]">
            <Link
              to="/legal/terms"
              className={kind === "terms" ? "text-brand-600 font-semibold" : "text-ink-500 hover:text-ink-800"}
            >
              服务协议
            </Link>
            <Link
              to="/legal/privacy"
              className={kind === "privacy" ? "text-brand-600 font-semibold" : "text-ink-500 hover:text-ink-800"}
            >
              隐私政策
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}
