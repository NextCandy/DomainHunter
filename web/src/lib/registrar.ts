/**
 * 注册商名字的显示规则。
 *
 * RDAP/WHOIS 返回的是法律实体全名（"Alibaba Cloud Computing (Beijing) Co., Ltd."、
 * "TurnCommerce, Inc. DBA NameBright.com"），在 112 项的筛选列表和表格单元格里
 * 完全不可读。这里把全名压成人认得出的品牌短名，全名交给 title 属性。
 *
 * 两层策略：
 * ① 显式映射覆盖高频项和"名字里认不出品牌"的情况（中文实体名、DBA 商号）；
 * ② 通用规则兜底——剥掉法律后缀和域名尾巴，未知注册商也能得到像样的短名。
 */

/** 显式映射：键为全名的小写形式，前缀匹配（注册局偶尔会在尾部加地区后缀）。 */
const EXPLICIT: Array<[string, string]> = [
  ["godaddy", "GoDaddy"],
  ["ename technology", "eName"],
  ["厦门易名科技", "eName"],
  ["spaceship", "Spaceship"],
  ["turncommerce", "NameBright"],
  ["dynadot", "Dynadot"],
  ["alibaba cloud computing", "阿里云"],
  ["阿里云计算", "阿里云"],
  ["hichina", "阿里云"],
  ["gname", "Gname"],
  ["cloudflare", "Cloudflare"],
  ["tucows", "Tucows"],
  ["namecheap", "Namecheap"],
  ["csc corporate domains", "CSC"],
  ["namesilo", "NameSilo"],
  ["network solutions", "Network Solutions"],
  ["domain.com", "Domain.com"],
  ["squarespace", "Squarespace"],
  ["enom", "eNom"],
  ["ionos", "IONOS"],
  ["成都西维数码", "西部数码"],
  ["west263", "西部数码"],
  ["xin net technology", "新网"],
  ["xiamen 35.com", "35互联"],
  ["porkbun", "Porkbun"],
  ["name.com", "Name.com"],
  ["moniker", "Moniker"],
  ["dnspod", "DNSPod"],
  ["amazon registrar", "Amazon"],
  ["浙江贰贰网络", "22net"],
  ["22net", "22net"],
  ["unstoppable domains", "Unstoppable"],
  ["megazone", "HOSTING.KR"],
  ["key-systems", "Key-Systems"],
  ["internetx", "InterNetX"],
  ["gandi", "Gandi"],
  ["dominet", "Dominet"],
  ["markmonitor", "MarkMonitor"],
  ["google llc", "Google"],
  ["tencent cloud", "腾讯云"],
  ["腾讯云计算", "腾讯云"],
  ["west.cn", "西部数码"],
  ["bizcn", "商务中国"],
  ["todaynic", "商务中国"],
  ["net.cn", "阿里云"],
];

/** 法律实体后缀：按出现顺序逐个剥掉，不区分大小写。 */
const LEGAL_SUFFIX =
  /(,?\s*(inc|llc|ltd|limited|corp|corporation|co|company|gmbh|sas|sarl|s\.a\.|se|ab|bv|b\.v\.|pte|pty|plc|kg|oy|as|a\/s)\.?)+\s*$/i;

/** 中文法律实体后缀。 */
const LEGAL_SUFFIX_CN = /(股份)?有限(责任)?公司$|集团$/;

/**
 * 把注册商全名压成品牌短名。
 * 无法识别时返回去掉法律后缀的原名，最差情况返回原样——绝不返回空串。
 */
export function registrarShortName(full?: string | null): string {
  const raw = (full ?? "").trim();
  if (!raw) return "";

  // 后端对不提供注册商信息的后缀写入的是一句中文说明，不是注册商名，原样透传。
  if (raw.startsWith("该后缀不支持")) return raw;

  const lower = raw.toLowerCase();
  for (const [needle, short] of EXPLICIT) {
    if (lower.includes(needle)) return short;
  }

  // "TurnCommerce, Inc. DBA NameBright.com"——DBA 之后才是用户认识的商号。
  const dba = raw.split(/\s+(?:dba|d\/b\/a)\s+/i);
  let name = dba.length > 1 ? dba[dba.length - 1] : raw;

  // "Domain.com - Network Solutions, LLC" 这类用连字符并列的，取前半段品牌。
  const dashed = name.split(/\s+[-–—]\s+/);
  if (dashed.length > 1) name = dashed[0];

  name = name
    .replace(/（.*?）|\(.*?\)/g, " ") // 括号里是地区或网址注解
    .replace(/\s+/g, " ")
    .trim();
  name = name.replace(LEGAL_SUFFIX, "").trim();
  name = name.replace(LEGAL_SUFFIX_CN, "").trim();
  name = name.replace(/[,，.。]+$/, "").trim();
  // "GoDaddy.com" / "Gname.com" 里的 .com 是公司名的一部分而不是后缀，
  // 但在筛选列表里全都删掉更整齐——Name.com / Domain.com 已由显式映射兜住。
  name = name.replace(/\.(com|net|cn|kr|io)$/i, "").trim();

  return name || raw;
}

/**
 * 常见注册商的色点。用语义 token 而不是品牌官方色：
 * 这套设计语言只有一个彩色（action 琥珀），色点靠中性色的深浅区分即可，
 * 目的是让用户在长列表里快速认出"这几家是我的主力"，不是还原品牌 VI。
 */
const ACCENTED = new Set(["GoDaddy", "阿里云", "Dynadot", "Namecheap", "Cloudflare", "Spaceship", "eName"]);

/** 该注册商是否值得在表格里给一个色点。 */
export function isMajorRegistrar(full?: string | null): boolean {
  return ACCENTED.has(registrarShortName(full));
}
