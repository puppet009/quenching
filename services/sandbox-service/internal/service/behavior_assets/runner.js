#!/usr/bin/env node
/**
 * behavior-runner：真实用户行为模拟引擎（行为模拟第二批）。
 *
 * 职责：
 *   1. 读取 flow.json（由 content-service 模板的 behavior_flow 生成，
 *      ConfigMap 挂载到 FLOW_CONFIG_PATH）；
 *   2. 用 Playwright 驱动项目自己的前端页面，按顺序循环执行 steps
 *      （goto / click / fill / wait / assert），模拟真实用户操作；
 *   3. 全程订阅浏览器侧失败信号：
 *      - step_failure   ：步骤断言/操作失败（含超时、元素找不到）
 *      - page_error     ：页面未捕获 JS 异常（白屏/崩溃类）
 *      - console_error  ：console.error 输出
 *      - http_error     ：非 2xx（含 3xx 之外的失败）HTTP 响应
 *      - request_failed ：请求未完成（网络中断/DNS/连接失败）
 *   4. 信号统一 POST 到 SIGNAL_ENDPOINT；端点留空时只打印（不落假数据）。
 *
 * 设计约束：
 *   - runner 只观测、只上报真实发生的事件，绝不合成信号；
 *   - 单轮失败不中断进程：关闭页面后下一轮重新打开浏览器上下文，
 *     保证故障修复后行为流自动恢复，由告警端按静默窗口判定恢复；
 *   - 浏览器二进制使用镜像内置路径（PLAYWRIGHT_BROWSERS_PATH=/ms-playwright），
 *     playwright-core npm 包由启动命令在本地目录安装后通过 NODE_PATH 引入。
 */

'use strict';

const fs = require('fs');
const path = require('path');

// playwright-core 由启动命令安装到本地 node_modules（NODE_PATH 已指向它）。
let chromium;
try {
  ({ chromium } = require('playwright-core'));
} catch (err) {
  console.error('[behavior] 无法加载 playwright-core，请确认启动安装步骤成功:', err.message);
  process.exit(1);
}

const ENTRY_URL = process.env.ENTRY_URL || '';
const SIGNAL_ENDPOINT = process.env.SIGNAL_ENDPOINT || '';
const SESSION_ID = process.env.SESSION_ID || '0';
const FLOW_CONFIG_PATH = process.env.FLOW_CONFIG_PATH || '/etc/behavior/flow.json';

const DEFAULT_TIMEOUT_MS = 15000;
const NAV_TIMEOUT_MS = 30000;

function loadFlow() {
  const raw = fs.readFileSync(FLOW_CONFIG_PATH, 'utf8');
  const flow = JSON.parse(raw);
  if (!flow || !Array.isArray(flow.steps) || flow.steps.length === 0) {
    throw new Error('flow.json 缺少 steps 或 steps 为空');
  }
  flow.interval_ms = Number.isFinite(flow.interval_ms) ? flow.interval_ms : 5000;
  flow.timeout_ms = Number.isFinite(flow.timeout_ms) ? flow.timeout_ms : DEFAULT_TIMEOUT_MS;
  return flow;
}

function nowISO() {
  return new Date().toISOString();
}

/**
 * 上报一条行为信号。SIGNAL_ENDPOINT 为空时仅打印。
 * 上报失败只打印、不抛出（监控链路不能拖垮行为流本身）。
 */
async function report(type, payload) {
  const signal = Object.assign(
    {
      sandbox_id: Number(SESSION_ID),
      session_id: Number(SESSION_ID),
      type,
      severity: 'error',
      timestamp: nowISO(),
    },
    payload
  );

  const line = JSON.stringify(signal);
  console.log('[behavior-signal] ' + line);

  if (!SIGNAL_ENDPOINT) {
    return;
  }
  try {
    await fetch(SIGNAL_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: line,
    });
  } catch (err) {
    console.error('[behavior] 信号上报失败:', err.message);
  }
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * 把 Playwright 错误整理成可上报的消息文本。
 */
function errMessage(err) {
  if (!err) return 'unknown error';
  return String(err && err.message ? err.message : err).split('\n')[0].slice(0, 500);
}

/**
 * 在一个浏览器上下文中执行完整一轮 steps。
 * 浏览器侧订阅（pageerror / console / response / requestfailed）
 * 在每个步骤期间持续生效。
 */
async function runOnce(browser, flow) {
  const context = await browser.newContext();
  context.setDefaultTimeout(flow.timeout_ms);
  context.setDefaultNavigationTimeout(NAV_TIMEOUT_MS);

  const page = await context.newPage();

  // 当前执行步骤名；事件回调触发时据此定位信号发生在哪个步骤。
  let currentStep = '';

  // --- 浏览器侧信号订阅 ---
  page.on('pageerror', (err) => {
    void report('page_error', {
      step: currentStep,
      message: errMessage(err),
      url: page.url(),
    });
  });

  page.on('console', (msg) => {
    if (msg.type() !== 'error') {
      return;
    }
    void report('console_error', {
      step: currentStep,
      message: errMessage(msg.text()),
      url: page.url(),
    });
  });

  page.on('response', (res) => {
    const status = res.status();
    // 4xx/5xx 都是用户视角的失败；3xx 由浏览器自动处理，不算错误。
    if (status < 400) {
      return;
    }
    void report('http_error', {
      step: currentStep,
      message: 'HTTP ' + status + ' ' + (res.statusText() || ''),
      url: res.url(),
      status,
    });
  });

  page.on('requestfailed', (req) => {
    const failure = req.failure();
    void report('request_failed', {
      step: currentStep,
      message: (failure && failure.errorText) || 'request failed',
      url: req.url(),
    });
  });

  try {
    for (const step of flow.steps) {
      currentStep = step.name || step.action;
      await executeStep(page, step, flow);
    }
  } finally {
    await context.close().catch(() => {});
  }
}

/**
 * 解析 goto 的目标 URL：绝对地址原样使用，相对地址基于 ENTRY_URL 解析。
 */
function resolveURL(target) {
  try {
    return new URL(target, ENTRY_URL).toString();
  } catch (err) {
    return target;
  }
}

/**
 * 执行单个步骤；任何失败转为 step_failure 信号后向上抛出，
 * 由 runOnce 的调用方结束本轮（后续步骤依赖前序页面状态，继续执行无意义）。
 */
async function executeStep(page, step, flow) {
  const stepTimeout = step.timeout_ms || flow.timeout_ms;
  try {
    switch (step.action) {
      case 'goto': {
        await page.goto(resolveURL(step.url), {
          timeout: NAV_TIMEOUT_MS,
          waitUntil: 'domcontentloaded',
        });
        break;
      }
      case 'click': {
        await page.locator(step.selector).first().click({ timeout: stepTimeout });
        break;
      }
      case 'fill': {
        await page.locator(step.selector).first().fill(step.value || '', {
          timeout: stepTimeout,
        });
        break;
      }
      case 'wait': {
        if (step.selector) {
          await page
            .locator(step.selector)
            .first()
            .waitFor({ state: 'visible', timeout: stepTimeout });
        } else {
          await sleep(step.ms && step.ms > 0 ? step.ms : 1000);
        }
        break;
      }
      case 'assert': {
        const locator = page.locator(step.selector).first();
        switch (step.expect) {
          case 'visible':
            await locator.waitFor({ state: 'visible', timeout: stepTimeout });
            break;
          case 'hidden':
            await locator.waitFor({ state: 'hidden', timeout: stepTimeout });
            break;
          case 'text':
            await locator.waitFor({ state: 'visible', timeout: stepTimeout });
            const text = await locator.textContent();
            if (!text || text.indexOf(step.value) === -1) {
              throw new Error(
                '断言失败：期望包含文本 ' + JSON.stringify(step.value) + '，实际 ' + JSON.stringify(text || '')
              );
            }
            break;
          default:
            throw new Error('不支持的 expect: ' + step.expect);
        }
        break;
      }
      default:
        throw new Error('不支持的 action: ' + step.action);
    }
  } catch (err) {
    await report('step_failure', {
      step: step.name || step.action,
      action: step.action,
      message: errMessage(err),
      url: page.url(),
    });
    throw err;
  }
}

async function main() {
  if (process.env.BEHAVIOR_COMMAND_URL) return require("./command_runner.js").main(chromium);
  const flow = loadFlow();

  if (!ENTRY_URL) {
    console.error('[behavior] 未配置 ENTRY_URL，无法执行有效行为流');
    process.exit(1);
  }
  console.log('[behavior] 启动行为模拟，入口:', ENTRY_URL, '步骤数:', flow.steps.length);

  const browser = await chromium.launch({
    headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage'],
  });

  const shutdown = async () => {
    console.log('[behavior] 收到退出信号，关闭浏览器');
    await browser.close().catch(() => {});
    process.exit(0);
  };
  process.on('SIGTERM', () => void shutdown());
  process.on('SIGINT', () => void shutdown());

  for (;;) {
    try {
      await runOnce(browser, flow);
    } catch (err) {
      // 单步失败已在 executeStep 内上报 step_failure；这里仅记录本轮终止。
      console.error('[behavior] 本轮行为流中断:', errMessage(err));
    }
    await sleep(flow.interval_ms);
  }
}

main().catch((err) => {
  console.error('[behavior] 运行时致命错误:', errMessage(err));
  process.exit(1);
});
