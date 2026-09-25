import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { Overview } from "./overview";
const { call, success } = vi.hoisted(()=>({call:vi.fn(),success:vi.fn()}));
vi.mock("sonner",()=>({toast:{success,error:vi.fn()}}));
vi.mock("@/lib/company",async original=>({...(await original<typeof import("@/lib/company")>()),company:(...args:unknown[])=>call(...args)}));
afterEach(()=>{cleanup();vi.clearAllMocks();});
it("requires a reason and refreshes the company summary after bounded index recovery",async()=>{
 let failed=2;
 call.mockImplementation(async(path:string)=>{
  if(path==="/index/retry"){failed=0;return {requeued:2,limit:100};}
  return {mailboxes:3,active_employees:3,pending_invitations:0,queued:0,uncertain:0,index_failed:failed};
 });
 render(<Overview/>);
 const button=await screen.findByRole("button",{name:"Retry failed indexes"});
 expect(button).toBeDisabled();
 fireEvent.change(screen.getByLabelText("Recovery reason"),{target:{value:"Storage access restored and verified"}});
 fireEvent.click(button);
 await waitFor(()=>expect(call).toHaveBeenCalledWith("/index/retry",{method:"POST",body:{reason:"Storage access restored and verified"}}));
 await waitFor(()=>expect(screen.queryByRole("button",{name:"Retry failed indexes"})).toBeNull());
 expect(success).toHaveBeenCalledWith("Requeued 2 index jobs");
});
