import {Employees} from "@/features/company/employees";
import {CompanyShell} from "@/features/company/shell";
export default function Page(){return <CompanyShell title={["员工与离职交接","Employees and offboarding"]}><Employees/></CompanyShell>;}
