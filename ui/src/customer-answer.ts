import { createElement as h } from "react";
import { parseCustomerAnswerV1 } from "./contract_types/customer_answer_v1.js";
import { ContractError } from "./contract_types/_runtime.js";

export function CustomerAnswer({ answer: input }: { answer: unknown }) {
  let answer;
  try {
    answer = parseCustomerAnswerV1(input);
  } catch (error) {
    if (!(error instanceof ContractError)) throw error;
    return h("p", { role: "status" }, "This result is unavailable.");
  }
  return h("section", { "aria-label": "Answer" },
    h("h2", null, "What I found"),
    ...answer.found.map((item, i) => h("p", { key: i }, item)),
    answer.not_known.length ? h("h2", null, "What I do not know yet") : null,
    ...answer.not_known.map((item, i) => h("p", { key: i }, item.what, " ", item.why)),
    h("h2", null, "What I did"),
    h("p", null, answer.done.summary),
    answer.done.verified ? h("span", null, "Verified") : null,
    ...answer.advice.do_not.map((item, i) => h("p", { key: `not-${i}` }, item)),
    ...answer.advice.do.map((item, i) => h("p", { key: `do-${i}` }, item)),
    h("h2", null, "What happens next"),
    h("p", null, answer.next.what, answer.next.when ? ` ${answer.next.when}` : ""),
    answer.case_ref_masked === undefined ? null : h("p", null, "Case reference: ", answer.case_ref_masked),
  );
}
