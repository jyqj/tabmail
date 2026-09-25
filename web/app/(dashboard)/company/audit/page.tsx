import {CompanyAudit} from "@/features/company/audit";
import {CompanyShell} from "@/features/company/shell";
export default function Page(){return <CompanyShell title={["公司管理审计","Company administration audit"]}><CompanyAudit/></CompanyShell>;}
