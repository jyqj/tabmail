import {MailboxAdmin} from "@/features/company/mailbox-admin";
import {CompanyShell} from "@/features/company/shell";
export default function Page(){return <CompanyShell title={["邮箱与授权","Mailboxes and grants"]}><MailboxAdmin/></CompanyShell>;}
