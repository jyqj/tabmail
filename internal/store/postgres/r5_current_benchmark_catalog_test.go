//go:build r5benchmark

package postgres

// C18-07 SOURCE ONLY. This is a new, deliberately separate qualification path,
// Not yet SQL-qualified; V5 is still SOURCE, with no SQL qualification claimed.
// Not the seven-collection historical inventory and not a benchmark admission.
// No candidate produced here is a signed reference, a C18-05/06 clock receipt,
// or permission to run S/M/L. A separately authorized sole runner and independent
// reader/evidence review must qualify and install any future reference.
//
// Catalog format v5 targets PostgreSQL 16 (the checkout's compose major). Other
// majors and unsupported catalog object classes fail closed, not partially pass.
// Semantics follow https://www.postgresql.org/docs/16/catalogs.html .
//
// Normalization: OIDs become qualified identities; raw parse trees become server
// deparsed definitions with a fixed metadata-only search_path. Owners, grantees,
// ACL null-vs-explicit state, settings, typmods and enable/validation state are
// NEVER erased. Internal RI trigger names contain allocated OIDs; only those
// generated names are replaced by constraint/table/function/type identities,
// including the matching dependency identities (not arbitrary text scrubbing).
// The owned datname stays in the envelope, outside the structural
// digest. Associated TOAST schema/options/index state is retained, not discarded
// with its generated OID/name. Physical files, estimates/statistics, transaction IDs, sequence current
// values and Goose applied timestamps are not schema definitions. Exact Goose
// version/order/applied state IS retained. ACL arrays and other meaningful arrays
// retain order; only collection rows and JSON object keys are sorted bytewise.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"tabmail/internal/config"
)

const c18CatalogFormat = "r5_current18_catalog_candidate_pg16_v5"
const c18CatalogCanonical = "go_encoding_json_utf8_sorted_keys_sorted_rows_use_number_v1"
const c18CatalogMaintenance = "no_explicit_analyze_or_planner_or_durability_override"

type c18CatalogRow map[string]any
type c18Catalog map[string][]c18CatalogRow
type c18CatalogQuery struct{ Name, Keys, SQL string }

// Each collection declares its complete shape. Null is a value, not a missing
// key. Sections unsupported by this version are rejected by the scope query.
var c18CatalogQueries = []c18CatalogQuery{
	{"schemas", "name owner acl", `SELECT jsonb_build_object('name',nspname,'owner',pg_get_userbyid(nspowner),'acl',nspacl::text)::text FROM pg_namespace WHERE nspname='public'`},
	{"extensions", "name schema version owner relocatable config conditions", `SELECT jsonb_build_object('name',e.extname,'schema',n.nspname,'version',e.extversion,'owner',pg_get_userbyid(e.extowner),'relocatable',e.extrelocatable,'config',(SELECT jsonb_agg(x::regclass::text ORDER BY ord) FROM unnest(e.extconfig) WITH ORDINALITY u(x,ord)),'conditions',e.extcondition)::text FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace`},
	{"tables", "table kind persistence owner acl rls forced_rls replica_identity options access_method tablespace partition_bound partition_key view_definition populated checks has_rules has_triggers is_partition", `SELECT jsonb_build_object('table',c.relname,'kind',c.relkind,'persistence',c.relpersistence,'owner',pg_get_userbyid(c.relowner),'acl',c.relacl::text,'rls',c.relrowsecurity,'forced_rls',c.relforcerowsecurity,'replica_identity',c.relreplident,'options',c.reloptions,'access_method',a.amname,'tablespace',ts.spcname,'partition_bound',pg_get_expr(c.relpartbound,c.oid,false),'partition_key',pg_get_partkeydef(c.oid),'view_definition',CASE WHEN c.relkind IN ('v','m') THEN pg_get_viewdef(c.oid,false) ELSE NULL END,'populated',c.relispopulated,'checks',c.relchecks,'has_rules',c.relhasrules,'has_triggers',c.relhastriggers,'is_partition',c.relispartition)::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_am a ON a.oid=c.relam LEFT JOIN pg_tablespace ts ON ts.oid=c.reltablespace WHERE n.nspname='public' AND c.relkind NOT IN ('i','I','S')`},
	{"columns", "table name position type formatted_type typmod nullable default collation identity generated dropped dimensions length by_value alignment storage compression statistics inherited local acl options fdw_options missing_value", `SELECT jsonb_build_object('table',c.relname,'name',a.attname,'position',a.attnum,'type',a.atttypid::regtype::text,'formatted_type',format_type(a.atttypid,a.atttypmod),'typmod',a.atttypmod,'nullable',NOT a.attnotnull,'default',pg_get_expr(d.adbin,d.adrelid,false),'collation',CASE WHEN a.attcollation=0 THEN NULL ELSE a.attcollation::regcollation::text END,'identity',a.attidentity,'generated',a.attgenerated,'dropped',a.attisdropped,'dimensions',a.attndims,'length',a.attlen,'by_value',a.attbyval,'alignment',a.attalign,'storage',a.attstorage,'compression',a.attcompression,'statistics',a.attstattarget,'inherited',a.attinhcount,'local',a.attislocal,'acl',a.attacl::text,'options',a.attoptions,'fdw_options',a.attfdwoptions,'missing_value',a.attmissingval::text)::text FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE n.nspname='public' AND a.attnum>0`},
	{"constraints", "table domain name type definition validated deferrable initially_deferred columns referenced_table referenced_columns fk_update_action fk_delete_action fk_match_type local inherited no_inherit parent index", `SELECT jsonb_build_object('table',CASE WHEN p.conrelid=0 THEN NULL ELSE p.conrelid::regclass::text END,'domain',CASE WHEN p.contypid=0 THEN NULL ELSE p.contypid::regtype::text END,'name',p.conname,'type',p.contype,'definition',pg_get_constraintdef(p.oid,false),'validated',p.convalidated,'deferrable',p.condeferrable,'initially_deferred',p.condeferred,'columns',(SELECT jsonb_agg(a.attname ORDER BY u.ord) FROM unnest(p.conkey) WITH ORDINALITY u(num,ord) JOIN pg_attribute a ON a.attrelid=p.conrelid AND a.attnum=u.num),'referenced_table',CASE WHEN p.confrelid=0 THEN NULL ELSE p.confrelid::regclass::text END,'referenced_columns',(SELECT jsonb_agg(a.attname ORDER BY u.ord) FROM unnest(p.confkey) WITH ORDINALITY u(num,ord) JOIN pg_attribute a ON a.attrelid=p.confrelid AND a.attnum=u.num),'fk_update_action',p.confupdtype,'fk_delete_action',p.confdeltype,'fk_match_type',p.confmatchtype,'local',p.conislocal,'inherited',p.coninhcount,'no_inherit',p.connoinherit,'parent',CASE WHEN p.conparentid=0 THEN NULL ELSE (pg_identify_object('pg_constraint'::regclass,p.conparentid,0)).identity END,'index',CASE WHEN p.conindid=0 THEN NULL ELSE p.conindid::regclass::text END)::text FROM pg_constraint p JOIN pg_namespace n ON n.oid=p.connamespace WHERE n.nspname='public'`},
	{"indexes", "table name definition valid ready live unique primary exclusion immediate clustered replica_identity check_xmin nulls_not_distinct keys key_count attribute_count options persistence tablespace access_method collations opclasses predicate expressions", `SELECT jsonb_build_object('table',c.relname,'name',ic.relname,'definition',pg_get_indexdef(i.indexrelid,0,false),'valid',i.indisvalid,'ready',i.indisready,'live',i.indislive,'unique',i.indisunique,'primary',i.indisprimary,'exclusion',i.indisexclusion,'immediate',i.indimmediate,'clustered',i.indisclustered,'replica_identity',i.indisreplident,'check_xmin',i.indcheckxmin,'nulls_not_distinct',i.indnullsnotdistinct,'keys',i.indkey::text,'key_count',i.indnkeyatts,'attribute_count',i.indnatts,'options',ic.reloptions,'persistence',ic.relpersistence,'tablespace',ts.spcname,'access_method',am.amname,'collations',(SELECT jsonb_agg(CASE WHEN x=0 THEN NULL ELSE x::regcollation::text END ORDER BY ord) FROM unnest(i.indcollation) WITH ORDINALITY u(x,ord)),'opclasses',(SELECT jsonb_agg((pg_identify_object('pg_opclass'::regclass,x,0)).identity ORDER BY ord) FROM unnest(i.indclass) WITH ORDINALITY u(x,ord)),'predicate',pg_get_expr(i.indpred,i.indrelid,false),'expressions',pg_get_expr(i.indexprs,i.indrelid,false))::text FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_class ic ON ic.oid=i.indexrelid JOIN pg_am am ON am.oid=ic.relam LEFT JOIN pg_tablespace ts ON ts.oid=ic.reltablespace WHERE n.nspname='public'`},
	{"triggers", "table name function definition enabled deferrable initially_deferred type_bits internal columns arguments condition old_table new_table constraint parent", `SELECT jsonb_build_object('table',c.relname,'name',CASE WHEN t.tgisinternal THEN 'internal:'||(pg_identify_object('pg_constraint'::regclass,t.tgconstraint,0)).identity||':'||t.tgrelid::regclass::text||':'||t.tgfoid::regprocedure::text||':'||t.tgtype::text ELSE t.tgname END,'function',t.tgfoid::regprocedure::text,'definition',CASE WHEN t.tgisinternal THEN replace(pg_get_triggerdef(t.oid,false),quote_ident(t.tgname),quote_ident('internal:'||(pg_identify_object('pg_constraint'::regclass,t.tgconstraint,0)).identity||':'||t.tgrelid::regclass::text||':'||t.tgfoid::regprocedure::text||':'||t.tgtype::text)) ELSE pg_get_triggerdef(t.oid,false) END,'enabled',t.tgenabled,'deferrable',t.tgdeferrable,'initially_deferred',t.tginitdeferred,'type_bits',t.tgtype,'internal',t.tgisinternal,'columns',t.tgattr::text,'arguments',encode(t.tgargs,'hex'),'condition',CASE WHEN t.tgqual IS NULL THEN NULL ELSE pg_get_triggerdef(t.oid,false) END,'old_table',t.tgoldtable,'new_table',t.tgnewtable,'constraint',CASE WHEN t.tgconstraint=0 THEN NULL ELSE (pg_identify_object('pg_constraint'::regclass,t.tgconstraint,0)).identity END,'parent',CASE WHEN t.tgparentid=0 THEN NULL ELSE (pg_identify_object('pg_trigger'::regclass,t.tgparentid,0)).identity END)::text FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'`},
	{"sequences", "name type start min max increment cycle cache owner owner_table owner_column ownership_dependency_kind persistence acl options", `SELECT jsonb_build_object('name',c.relname,'type',s.seqtypid::regtype::text,'start',s.seqstart,'min',s.seqmin,'max',s.seqmax,'increment',s.seqincrement,'cycle',s.seqcycle,'cache',s.seqcache,'owner',pg_get_userbyid(c.relowner),'owner_table',d.refobjid::regclass::text,'owner_column',a.attname,'ownership_dependency_kind',d.deptype,'persistence',c.relpersistence,'acl',c.relacl::text,'options',c.reloptions)::text FROM pg_sequence s JOIN pg_class c ON c.oid=s.seqrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_depend d ON d.classid='pg_class'::regclass AND d.objid=s.seqrelid AND d.refclassid='pg_class'::regclass AND d.deptype IN ('a','i') LEFT JOIN pg_attribute a ON a.attrelid=d.refobjid AND a.attnum=d.refobjsubid WHERE n.nspname='public'`},
	{"functions", "schema name identity_arguments return_type language definition security_definer volatility strict parallel leakproof configuration owner acl cost rows kind returns_set support binary", `SELECT jsonb_build_object('schema',n.nspname,'name',p.proname,'identity_arguments',pg_get_function_identity_arguments(p.oid),'return_type',pg_get_function_result(p.oid),'language',l.lanname,'definition',pg_get_functiondef(p.oid),'security_definer',p.prosecdef,'volatility',p.provolatile,'strict',p.proisstrict,'parallel',p.proparallel,'leakproof',p.proleakproof,'configuration',p.proconfig,'owner',pg_get_userbyid(p.proowner),'acl',p.proacl::text,'cost',p.procost,'rows',p.prorows,'kind',p.prokind,'returns_set',p.proretset,'support',p.prosupport::regprocedure::text,'binary',p.probin)::text FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang WHERE n.nspname='public'`},
	{"types", "name kind category owner acl defined length by_value alignment storage preferred delimiter relation element array base typmod dimensions not_null default default_expression collation input output receive send typmod_in typmod_out analyze subscript", `SELECT jsonb_build_object('name',t.typname,'kind',t.typtype,'category',t.typcategory,'owner',pg_get_userbyid(t.typowner),'acl',t.typacl::text,'defined',t.typisdefined,'length',t.typlen,'by_value',t.typbyval,'alignment',t.typalign,'storage',t.typstorage,'preferred',t.typispreferred,'delimiter',t.typdelim,'relation',CASE WHEN t.typrelid=0 THEN NULL ELSE t.typrelid::regclass::text END,'element',CASE WHEN t.typelem=0 THEN NULL ELSE t.typelem::regtype::text END,'array',CASE WHEN t.typarray=0 THEN NULL ELSE t.typarray::regtype::text END,'base',CASE WHEN t.typbasetype=0 THEN NULL ELSE t.typbasetype::regtype::text END,'typmod',t.typtypmod,'dimensions',t.typndims,'not_null',t.typnotnull,'default',t.typdefault,'default_expression',pg_get_expr(t.typdefaultbin,0,false),'collation',CASE WHEN t.typcollation=0 THEN NULL ELSE t.typcollation::regcollation::text END,'input',t.typinput::regprocedure::text,'output',t.typoutput::regprocedure::text,'receive',t.typreceive::regprocedure::text,'send',t.typsend::regprocedure::text,'typmod_in',t.typmodin::regprocedure::text,'typmod_out',t.typmodout::regprocedure::text,'analyze',t.typanalyze::regprocedure::text,'subscript',t.typsubscript::regprocedure::text)::text FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public'`},
	{"enums", "type label order", `SELECT jsonb_build_object('type',e.enumtypid::regtype::text,'label',e.enumlabel,'order',e.enumsortorder)::text FROM pg_enum e JOIN pg_type t ON t.oid=e.enumtypid JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public'`},
	{"collations", "name schema owner provider deterministic encoding lc_collate lc_ctype icu_locale icu_rules version actual_version", `SELECT jsonb_build_object('name',c.collname,'schema',n.nspname,'owner',pg_get_userbyid(c.collowner),'provider',c.collprovider,'deterministic',c.collisdeterministic,'encoding',c.collencoding,'lc_collate',c.collcollate,'lc_ctype',c.collctype,'icu_locale',c.colliculocale,'icu_rules',c.collicurules,'version',c.collversion,'actual_version',pg_collation_actual_version(c.oid))::text FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace WHERE n.nspname='public' OR c.oid IN (SELECT a.attcollation FROM pg_attribute a JOIN pg_class r ON r.oid=a.attrelid JOIN pg_namespace rn ON rn.oid=r.relnamespace WHERE rn.nspname='public') OR c.oid IN (SELECT unnest(i.indcollation) FROM pg_index i JOIN pg_class r ON r.oid=i.indrelid JOIN pg_namespace rn ON rn.oid=r.relnamespace WHERE rn.nspname='public')`},
	{"policies", "table name command permissive roles using with_check", `SELECT jsonb_build_object('table',p.polrelid::regclass::text,'name',p.polname,'command',p.polcmd,'permissive',p.polpermissive,'roles',(SELECT jsonb_agg(CASE WHEN r=0 THEN 'PUBLIC' ELSE pg_get_userbyid(r) END ORDER BY ord) FROM unnest(p.polroles) WITH ORDINALITY u(r,ord)),'using',pg_get_expr(p.polqual,p.polrelid,false),'with_check',pg_get_expr(p.polwithcheck,p.polrelid,false))::text FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'`},
	{"rules", "table name enabled definition", `SELECT jsonb_build_object('table',r.ev_class::regclass::text,'name',r.rulename,'enabled',r.ev_enabled,'definition',pg_get_ruledef(r.oid,false))::text FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'`},
	{"inheritance", "table parent sequence detach_pending", `SELECT jsonb_build_object('table',i.inhrelid::regclass::text,'parent',i.inhparent::regclass::text,'sequence',i.inhseqno,'detach_pending',i.inhdetachpending)::text FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'`},
	{"default_acl", "role schema object_type acl", `SELECT jsonb_build_object('role',pg_get_userbyid(a.defaclrole),'schema',n.nspname,'object_type',a.defaclobjtype,'acl',a.defaclacl::text)::text FROM pg_default_acl a LEFT JOIN pg_namespace n ON n.oid=a.defaclnamespace`},
	{"operators", "identity owner kind left_type right_type result_type function commutator negator restriction join_function can_merge can_hash", `SELECT jsonb_build_object('identity',o.oid::regoperator::text,'owner',pg_get_userbyid(o.oprowner),'kind',o.oprkind,'left_type',o.oprleft::regtype::text,'right_type',o.oprright::regtype::text,'result_type',o.oprresult::regtype::text,'function',o.oprcode::regprocedure::text,'commutator',o.oprcom::regoperator::text,'negator',o.oprnegate::regoperator::text,'restriction',o.oprrest::regprocedure::text,'join_function',o.oprjoin::regprocedure::text,'can_merge',o.oprcanmerge,'can_hash',o.oprcanhash)::text FROM pg_operator o JOIN pg_namespace n ON n.oid=o.oprnamespace WHERE n.nspname='public'`},
	{"opclasses", "identity owner family input_type key_type default", `SELECT jsonb_build_object('identity',(pg_identify_object('pg_opclass'::regclass,o.oid,0)).identity,'owner',pg_get_userbyid(o.opcowner),'family',(pg_identify_object('pg_opfamily'::regclass,o.opcfamily,0)).identity,'input_type',o.opcintype::regtype::text,'key_type',o.opckeytype::regtype::text,'default',o.opcdefault)::text FROM pg_opclass o JOIN pg_namespace n ON n.oid=o.opcnamespace WHERE n.nspname='public'`},
	{"opfamilies", "identity owner", `SELECT jsonb_build_object('identity',(pg_identify_object('pg_opfamily'::regclass,o.oid,0)).identity,'owner',pg_get_userbyid(o.opfowner))::text FROM pg_opfamily o JOIN pg_namespace n ON n.oid=o.opfnamespace WHERE n.nspname='public'`},
	{"am_operators", "family left_type right_type strategy purpose operator method sort_family", `SELECT jsonb_build_object('family',(pg_identify_object('pg_opfamily'::regclass,a.amopfamily,0)).identity,'left_type',a.amoplefttype::regtype::text,'right_type',a.amoprighttype::regtype::text,'strategy',a.amopstrategy,'purpose',a.amoppurpose,'operator',a.amopopr::regoperator::text,'method',m.amname,'sort_family',CASE WHEN a.amopsortfamily=0 THEN NULL ELSE (pg_identify_object('pg_opfamily'::regclass,a.amopsortfamily,0)).identity END)::text FROM pg_amop a JOIN pg_opfamily f ON f.oid=a.amopfamily JOIN pg_namespace n ON n.oid=f.opfnamespace JOIN pg_am m ON m.oid=a.amopmethod WHERE n.nspname='public'`},
	{"am_procedures", "family left_type right_type number function", `SELECT jsonb_build_object('family',(pg_identify_object('pg_opfamily'::regclass,a.amprocfamily,0)).identity,'left_type',a.amproclefttype::regtype::text,'right_type',a.amprocrighttype::regtype::text,'number',a.amprocnum,'function',a.amproc::regprocedure::text)::text FROM pg_amproc a JOIN pg_opfamily f ON f.oid=a.amprocfamily JOIN pg_namespace n ON n.oid=f.opfnamespace WHERE n.nspname='public'`},
	{"dependencies", "object_class object_type object_schema object_resolved object_in_scope object reference_class reference_type reference_schema reference_resolved reference_in_scope reference kind", c18CatalogObjectScopeCTE + `SELECT jsonb_build_object('object_class',d.classid::regclass::text,'object_type',o.type,'object_schema',ol.owner_schema,'object_resolved',ol.objid IS NOT NULL,'object_in_scope',os.objid IS NOT NULL,'object',COALESCE(os.stable_identity||CASE WHEN d.objsubid=0 THEN '' ELSE '.column:'||d.objsubid::text END,o.identity),'reference_class',d.refclassid::regclass::text,'reference_type',r.type,'reference_schema',rl.owner_schema,'reference_resolved',rl.objid IS NOT NULL,'reference_in_scope',rs.objid IS NOT NULL,'reference',COALESCE(rs.stable_identity||CASE WHEN d.refobjsubid=0 THEN '' ELSE '.column:'||d.refobjsubid::text END,r.identity),'kind',d.deptype)::text FROM pg_depend d LEFT JOIN object_scope os ON os.classid=d.classid AND os.objid=d.objid LEFT JOIN object_scope rs ON rs.classid=d.refclassid AND rs.objid=d.refobjid LEFT JOIN object_locations ol ON ol.classid=d.classid AND ol.objid=d.objid LEFT JOIN object_locations rl ON rl.classid=d.refclassid AND rl.objid=d.refobjid CROSS JOIN LATERAL pg_identify_object(d.classid,d.objid,d.objsubid) o CROSS JOIN LATERAL pg_identify_object(d.refclassid,d.refobjid,d.refobjsubid) r WHERE os.objid IS NOT NULL OR rs.objid IS NOT NULL`},
	{"comments", "class type schema object comment", c18CatalogObjectScopeCTE + `SELECT jsonb_build_object('class',d.classoid::regclass::text,'type',o.type,'schema',s.scope_schema,'object',COALESCE(s.stable_identity||CASE WHEN d.objsubid=0 THEN '' ELSE '.column:'||d.objsubid::text END,o.identity),'comment',d.description)::text FROM pg_description d JOIN object_scope s ON s.classid=d.classoid AND s.objid=d.objoid CROSS JOIN LATERAL pg_identify_object(d.classoid,d.objoid,d.objsubid) o`},
	{"roles", "name superuser inherit create_role create_db login replication bypass_rls connection_limit valid_until configuration", c18CatalogRolesCTE + `SELECT jsonb_build_object('name',r.rolname,'superuser',r.rolsuper,'inherit',r.rolinherit,'create_role',r.rolcreaterole,'create_db',r.rolcreatedb,'login',r.rolcanlogin,'replication',r.rolreplication,'bypass_rls',r.rolbypassrls,'connection_limit',r.rolconnlimit,'valid_until',r.rolvaliduntil::text,'configuration',r.rolconfig)::text FROM pg_roles r JOIN relevant v ON v.id=r.oid`},
	{"role_memberships", "role member grantor admin inherit set", c18CatalogRolesCTE + `SELECT jsonb_build_object('role',pg_get_userbyid(m.roleid),'member',pg_get_userbyid(m.member),'grantor',pg_get_userbyid(m.grantor),'admin',m.admin_option,'inherit',m.inherit_option,'set',m.set_option)::text FROM pg_auth_members m WHERE m.member IN (SELECT id FROM reachable) OR m.roleid IN (SELECT id FROM reachable)`},
	{"effective_acl", "kind object subobject grantor grantee privilege grantable", `SELECT jsonb_build_object('kind',o.kind,'object',o.identity,'subobject',o.subobject,'grantor',pg_get_userbyid(a.grantor),'grantee',CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE pg_get_userbyid(a.grantee) END,'privilege',a.privilege_type,'grantable',a.is_grantable)::text FROM (
 SELECT 'schema' AS kind,quote_ident(nspname) AS identity,NULL::text AS subobject,COALESCE(nspacl,acldefault('n',nspowner)) AS acl FROM pg_namespace WHERE nspname='public'
 UNION ALL SELECT 'database','owned_database',NULL,COALESCE(datacl,acldefault('d',datdba)) FROM pg_database WHERE datname=current_database()
 UNION ALL SELECT 'relation',c.oid::regclass::text,NULL,COALESCE(c.relacl,acldefault(CASE WHEN c.relkind='S' THEN 'S'::"char" ELSE 'r'::"char" END,c.relowner)) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind NOT IN ('i','I','c')
 UNION ALL SELECT 'function',p.oid::regprocedure::text,NULL,COALESCE(p.proacl,acldefault('f',p.proowner)) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'
 UNION ALL SELECT 'type',t.oid::regtype::text,NULL,COALESCE(t.typacl,acldefault('T',t.typowner)) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public'
 UNION ALL SELECT 'column',c.oid::regclass::text,a.attname,a.attacl FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND a.attnum>0
 ) o CROSS JOIN LATERAL aclexplode(o.acl) a`},

	{"toast", "table kind persistence owner acl options access_method tablespace rls forced_rls replica_identity columns indexes", `SELECT jsonb_build_object('table',p.oid::regclass::text,'kind',t.relkind,'persistence',t.relpersistence,'owner',pg_get_userbyid(t.relowner),'acl',t.relacl::text,'options',t.reloptions,'access_method',am.amname,'tablespace',ts.spcname,'rls',t.relrowsecurity,'forced_rls',t.relforcerowsecurity,'replica_identity',t.relreplident,'columns',(SELECT jsonb_agg(jsonb_build_object('name',a.attname,'position',a.attnum,'type',format_type(a.atttypid,a.atttypmod),'typmod',a.atttypmod,'not_null',a.attnotnull,'storage',a.attstorage,'compression',a.attcompression,'statistics',a.attstattarget,'options',a.attoptions,'acl',a.attacl::text) ORDER BY a.attnum) FROM pg_attribute a WHERE a.attrelid=t.oid AND a.attnum>0),'indexes',(SELECT jsonb_agg(jsonb_build_object('definition',replace(replace(pg_get_indexdef(i.indexrelid,0,false),quote_ident(ic.relname),quote_ident('toast_index:'||p.oid::regclass::text)),quote_ident(t.relname),quote_ident('toast:'||p.oid::regclass::text)),'valid',i.indisvalid,'ready',i.indisready,'live',i.indislive,'unique',i.indisunique,'options',ic.reloptions,'tablespace',its.spcname,'access_method',iam.amname) ORDER BY i.indisprimary DESC,pg_get_indexdef(i.indexrelid,0,false)) FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid JOIN pg_am iam ON iam.oid=ic.relam LEFT JOIN pg_tablespace its ON its.oid=ic.reltablespace WHERE i.indrelid=t.oid))::text FROM pg_class p JOIN pg_class t ON t.oid=p.reltoastrelid JOIN pg_am am ON am.oid=t.relam LEFT JOIN pg_tablespace ts ON ts.oid=t.reltablespace WHERE p.relnamespace='public'::regnamespace`},
	{"db_role_settings", "database_scope role_scope position name value known context", c18CatalogRolesCTE + `SELECT jsonb_build_object('database_scope',CASE WHEN d.setdatabase=0 THEN 'all_databases' ELSE 'owned_database' END,'role_scope',CASE WHEN d.setrole=0 THEN 'all_roles' ELSE pg_get_userbyid(d.setrole) END,'position',u.ord,'name',split_part(u.item,'=',1),'value',substr(u.item,strpos(u.item,'=')+1),'known',s.name IS NOT NULL,'context',s.context)::text FROM pg_db_role_setting d CROSS JOIN LATERAL unnest(d.setconfig) WITH ORDINALITY u(item,ord) LEFT JOIN pg_settings s ON lower(s.name)=lower(split_part(u.item,'=',1)) WHERE d.setdatabase=(SELECT oid FROM pg_database WHERE datname=current_database()) OR (d.setdatabase=0 AND (d.setrole=0 OR d.setrole IN (SELECT id FROM relevant)))`},

	{"external_objects", "class identity schema origin_kind origin_name origin_version artifact_sha256 definition", `SELECT '{}'::text WHERE false`},

	{"required_external_objects", "class identity schema profile required_by", `SELECT '{}'::text WHERE false`},

	{"goose", "id version applied", `SELECT jsonb_build_object('id',id,'version',version_id,'applied',is_applied)::text FROM public.goose_db_version`},
}

// pg_identify_object.schema is NULL for many relation-attached objects. Scope
// is defined by catalog addresses and owning relations/families, NOT that field.
// Stable identities are overrides only for OID-derived TOAST/RI names. All other
// identities remain the server's exact deparse. Both ends of pg_depend are read.
const c18CatalogObjectScopeCTE = `WITH public_rel AS (
 SELECT c.oid,c.reltoastrelid,c.oid::regclass::text AS identity FROM pg_class c WHERE c.relnamespace='public'::regnamespace
), toast_rel AS (
 SELECT t.oid,'toast:'||p.identity AS identity FROM public_rel p JOIN pg_class t ON t.oid=p.reltoastrelid
), object_scope(classid,objid,scope_schema,stable_identity) AS (
 SELECT 'pg_class'::regclass,oid,'public'::text,NULL::text FROM public_rel
 UNION SELECT 'pg_class'::regclass,oid,'public',identity FROM toast_rel
 UNION SELECT 'pg_class'::regclass,i.indexrelid,'public','index:'||t.identity FROM toast_rel t JOIN pg_index i ON i.indrelid=t.oid
 UNION SELECT 'pg_namespace'::regclass,oid,'public',NULL FROM pg_namespace WHERE nspname='public'
 UNION SELECT 'pg_proc'::regclass,oid,'public',NULL FROM pg_proc WHERE pronamespace='public'::regnamespace
 UNION SELECT 'pg_type'::regclass,oid,'public',NULL FROM pg_type WHERE typnamespace='public'::regnamespace
 UNION SELECT 'pg_constraint'::regclass,oid,'public',NULL FROM pg_constraint WHERE connamespace='public'::regnamespace OR conrelid IN (SELECT oid FROM public_rel)
 UNION SELECT 'pg_attrdef'::regclass,oid,'public',NULL FROM pg_attrdef WHERE adrelid IN (SELECT oid FROM public_rel)
 UNION SELECT 'pg_trigger'::regclass,t.oid,'public',CASE WHEN t.tgisinternal THEN 'internal:'||(pg_identify_object('pg_constraint'::regclass,t.tgconstraint,0)).identity||':'||t.tgrelid::regclass::text||':'||t.tgfoid::regprocedure::text||':'||t.tgtype::text ELSE NULL END FROM pg_trigger t WHERE tgrelid IN (SELECT oid FROM public_rel)
 UNION SELECT 'pg_rewrite'::regclass,oid,'public',NULL FROM pg_rewrite WHERE ev_class IN (SELECT oid FROM public_rel)
 UNION SELECT 'pg_policy'::regclass,oid,'public',NULL FROM pg_policy WHERE polrelid IN (SELECT oid FROM public_rel)
 UNION SELECT 'pg_operator'::regclass,oid,'public',NULL FROM pg_operator WHERE oprnamespace='public'::regnamespace
 UNION SELECT 'pg_opclass'::regclass,oid,'public',NULL FROM pg_opclass WHERE opcnamespace='public'::regnamespace
 UNION SELECT 'pg_opfamily'::regclass,oid,'public',NULL FROM pg_opfamily WHERE opfnamespace='public'::regnamespace
 UNION SELECT 'pg_amop'::regclass,a.oid,'public',NULL FROM pg_amop a JOIN pg_opfamily f ON f.oid=a.amopfamily WHERE f.opfnamespace='public'::regnamespace
 UNION SELECT 'pg_amproc'::regclass,a.oid,'public',NULL FROM pg_amproc a JOIN pg_opfamily f ON f.oid=a.amprocfamily WHERE f.opfnamespace='public'::regnamespace
 UNION SELECT 'pg_collation'::regclass,oid,'public',NULL FROM pg_collation WHERE collnamespace='public'::regnamespace
 UNION SELECT 'pg_default_acl'::regclass,oid,CASE WHEN defaclnamespace=0 THEN NULL ELSE 'public' END,NULL FROM pg_default_acl WHERE defaclnamespace IN (0,'public'::regnamespace)
 UNION SELECT 'pg_extension'::regclass,oid,NULL,NULL FROM pg_extension
), object_locations(classid,objid,owner_schema) AS (
 SELECT 'pg_class'::regclass,c.oid,n.nspname::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 UNION ALL SELECT 'pg_namespace'::regclass,oid,nspname::text FROM pg_namespace
 UNION ALL SELECT 'pg_proc'::regclass,c.oid,n.nspname FROM pg_proc c JOIN pg_namespace n ON n.oid=c.pronamespace
 UNION ALL SELECT 'pg_type'::regclass,c.oid,n.nspname FROM pg_type c JOIN pg_namespace n ON n.oid=c.typnamespace
 UNION ALL SELECT 'pg_constraint'::regclass,c.oid,n.nspname FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace
 UNION ALL SELECT 'pg_attrdef'::regclass,d.oid,n.nspname FROM pg_attrdef d JOIN pg_class c ON c.oid=d.adrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 UNION ALL SELECT 'pg_trigger'::regclass,d.oid,n.nspname FROM pg_trigger d JOIN pg_class c ON c.oid=d.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 UNION ALL SELECT 'pg_rewrite'::regclass,d.oid,n.nspname FROM pg_rewrite d JOIN pg_class c ON c.oid=d.ev_class JOIN pg_namespace n ON n.oid=c.relnamespace
 UNION ALL SELECT 'pg_policy'::regclass,d.oid,n.nspname FROM pg_policy d JOIN pg_class c ON c.oid=d.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 UNION ALL SELECT 'pg_operator'::regclass,c.oid,n.nspname FROM pg_operator c JOIN pg_namespace n ON n.oid=c.oprnamespace
 UNION ALL SELECT 'pg_opclass'::regclass,c.oid,n.nspname FROM pg_opclass c JOIN pg_namespace n ON n.oid=c.opcnamespace
 UNION ALL SELECT 'pg_opfamily'::regclass,c.oid,n.nspname FROM pg_opfamily c JOIN pg_namespace n ON n.oid=c.opfnamespace
 UNION ALL SELECT 'pg_amop'::regclass,a.oid,n.nspname FROM pg_amop a JOIN pg_opfamily f ON f.oid=a.amopfamily JOIN pg_namespace n ON n.oid=f.opfnamespace
 UNION ALL SELECT 'pg_amproc'::regclass,a.oid,n.nspname FROM pg_amproc a JOIN pg_opfamily f ON f.oid=a.amprocfamily JOIN pg_namespace n ON n.oid=f.opfnamespace
 UNION ALL SELECT 'pg_collation'::regclass,c.oid,n.nspname FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace
 UNION ALL SELECT 'pg_default_acl'::regclass,a.oid,n.nspname FROM pg_default_acl a LEFT JOIN pg_namespace n ON n.oid=a.defaclnamespace
 UNION ALL SELECT 'pg_extension'::regclass,oid,NULL FROM pg_extension
 UNION ALL SELECT 'pg_language'::regclass,oid,NULL FROM pg_language
 UNION ALL SELECT 'pg_am'::regclass,oid,NULL FROM pg_am
 UNION ALL SELECT 'pg_tablespace'::regclass,oid,NULL FROM pg_tablespace
) `

// Only roles governing this owned database (plus their membership closure),
// never pg_authid/passwords or unrelated role inventories.
// Bidirectional membership reachability matters: GRANT acl_grantee TO member
// changes the principals entitled to use the ACL without changing that ACL.
// Grantors are included as metadata, but do not seed another unrelated closure.
const c18CatalogRolesCTE = `WITH RECURSIVE reachable(id) AS (
 (SELECT refobjid FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND (dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) OR (dbid=0 AND classid='pg_database'::regclass AND objid=(SELECT oid FROM pg_database WHERE datname=current_database())))
 UNION SELECT u.roleid FROM pg_database d CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl,acldefault('d',d.datdba))) a CROSS JOIN LATERAL (VALUES(a.grantee),(a.grantor)) u(roleid) WHERE d.datname=current_database() AND u.roleid<>0
 UNION SELECT oid FROM pg_roles WHERE rolname IN (current_user,session_user,'pg_database_owner')
 UNION SELECT datdba FROM pg_database WHERE datname=current_database()
 UNION SELECT setrole FROM pg_db_role_setting WHERE setdatabase=(SELECT oid FROM pg_database WHERE datname=current_database()) AND setrole<>0)
 UNION SELECT CASE WHEN m.member=r.id THEN m.roleid ELSE m.member END FROM reachable r JOIN pg_auth_members m ON m.member=r.id OR m.roleid=r.id
), relevant(id) AS (
 SELECT id FROM reachable UNION SELECT m.grantor FROM pg_auth_members m WHERE m.member IN (SELECT id FROM reachable) OR m.roleid IN (SELECT id FROM reachable)
) `

// Unsupported objects are refused rather than silently omitted from a "full"
// digest. Namespaced objects with no outgoing dependency are covered explicitly.
// Infrastructure/system catalogs are not enumerated as application definitions.
const c18CatalogScopeSQL = `SELECT jsonb_build_object(
 'foreign_schema',(SELECT count(*) FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%' AND nspname NOT LIKE 'pg_temp_%'),
 'foreign_relations',(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND (c.relkind NOT IN ('r','i','S','v','m','c','p','I') OR c.relpersistence<>'p' OR c.reltablespace<>0)),
 'unsupported_functions',(SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.prokind NOT IN ('f','p')),
 'unsupported_types',(SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='public' AND (t.typtype NOT IN ('b','c','d','e') OR NOT t.typisdefined)),
 'extensions',(SELECT count(*) FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace WHERE e.extname NOT IN ('plpgsql','pgcrypto','pg_trgm') OR (e.extname IN ('pgcrypto','pg_trgm') AND n.nspname<>'public')),
 'event_triggers',(SELECT count(*) FROM pg_event_trigger),
 'foreign_servers',(SELECT count(*) FROM pg_foreign_server),
 'foreign_wrappers',(SELECT count(*) FROM pg_foreign_data_wrapper),
 'publications',(SELECT count(*) FROM pg_publication),
 'subscriptions',(SELECT count(*) FROM pg_subscription),
 'largeobjects',(SELECT count(*) FROM pg_largeobject_metadata),
 'security_labels',(SELECT count(*) FROM pg_seclabel),
 'transforms',(SELECT count(*) FROM pg_transform),
 'extended_statistics',(SELECT count(*) FROM pg_statistic_ext),
 'conversions',(SELECT count(*) FROM pg_conversion c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname='public'),
 'text_search',(SELECT count(*) FROM (SELECT cfgnamespace AS ns FROM pg_ts_config UNION ALL SELECT dictnamespace FROM pg_ts_dict UNION ALL SELECT prsnamespace FROM pg_ts_parser UNION ALL SELECT tmplnamespace FROM pg_ts_template) x JOIN pg_namespace n ON n.oid=x.ns WHERE n.nspname='public'),
 'casts',(SELECT count(*) FROM pg_cast c JOIN pg_type a ON a.oid=c.castsource JOIN pg_type b ON b.oid=c.casttarget WHERE a.typnamespace='public'::regnamespace OR b.typnamespace='public'::regnamespace),
 'custom_access_methods',(SELECT count(*) FROM pg_am WHERE amname NOT IN ('heap','btree','hash','gist','gin','spgist','brin')),
 'custom_languages',(SELECT count(*) FROM pg_language WHERE lanname NOT IN ('internal','c','sql','plpgsql')),
 'analyzed_public_tables',(SELECT count(*) FROM pg_stat_all_tables WHERE schemaname='public' AND (last_analyze IS NOT NULL OR analyze_count<>0))
)::text`

const c18CatalogSettingsSQL = `SELECT name,setting,unit,source,boot_val,reset_val,pending_restart FROM pg_settings WHERE name IN ('server_version','server_version_num','server_encoding','lc_collate','lc_ctype','TimeZone','DateStyle','search_path','fsync','full_page_writes','synchronous_commit','wal_level','autovacuum','default_table_access_method','default_tablespace','session_replication_role','row_security','default_transaction_read_only','default_toast_compression','work_mem','maintenance_work_mem','shared_buffers','effective_cache_size','random_page_cost','seq_page_cost','cpu_tuple_cost','cpu_index_tuple_cost','cpu_operator_cost','jit','max_parallel_workers_per_gather','max_parallel_workers','max_worker_processes','track_counts','lock_timeout','statement_timeout','idle_in_transaction_session_timeout','idle_session_timeout','shared_preload_libraries','session_preload_libraries','local_preload_libraries') OR name LIKE 'enable_%' ORDER BY name`

type c18CatalogSetting struct {
	Value   string  `json:"value"`
	Unit    *string `json:"unit"`
	Source  string  `json:"source"`
	Boot    string  `json:"boot"`
	Reset   string  `json:"reset"`
	Pending bool    `json:"pending_restart"`
}
type c18CatalogBinding struct {
	external *c18CatalogExternalReference

	AttemptID             string            `json:"attempt_id"`
	SourceSHA             string            `json:"source_sha"`
	SourcePolicy          string            `json:"source_policy"`
	SourceIdentityKind    string            `json:"source_identity_kind"`
	SourceManifestSHA256  string            `json:"source_manifest_sha256"`
	ContextSHA256         string            `json:"context_sha256"`
	CollectorSHA256       string            `json:"collector_sha256"`
	CatalogContractSHA256 string            `json:"catalog_contract_sha256"`
	MigrationSHA256       map[string]string `json:"migration_sha256"`
}
type c18CatalogCapture struct {
	Format                  string                       `json:"format"`
	Canonical               string                       `json:"canonical"`
	Binding                 c18CatalogBinding            `json:"binding"`
	BindingSHA256           string                       `json:"binding_sha256"`
	Datname                 string                       `json:"datname"`
	ServerVersion           string                       `json:"server_version"`
	ServerVersionNum        int                          `json:"server_version_num"`
	Database                c18CatalogRow                `json:"database"`
	Settings                map[string]c18CatalogSetting `json:"settings"`
	Stage                   string                       `json:"stage"`
	Maintenance             string                       `json:"maintenance"`
	Catalog                 c18Catalog                   `json:"catalog"`
	CatalogSHA256           string                       `json:"catalog_sha256"`
	EmbeddedMigrationSHA256 map[string]string            `json:"embedded_migration_sha256"`
}

func c18CatalogBytes(v any) ([]byte, error) {
	// Convert structs and RawMessages to plain maps first: encoding/json orders
	// struct fields by declaration, NOT lexically. Preserve numeric text exactly.
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var plain any
	if err = c18CatalogDecode(raw, &plain); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(plain); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}
func c18CatalogSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func c18CatalogDecode(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func c18CatalogHashValid(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == n && s == strings.ToLower(s)
}
func c18CatalogValidateBinding(b c18CatalogBinding) error {
	if b.SourcePolicy != "r5_current_local_inputs_v2" || b.SourceIdentityKind != "policy_bound_local_input_superset_sha1_v2" {
		return fmt.Errorf("binding source policy")
	}

	if b.AttemptID == "" || strings.ContainsAny(b.AttemptID, "\x00\r\n") || !c18CatalogHashValid(b.SourceSHA, 20) {
		return fmt.Errorf("binding source/attempt")
	}
	for _, s := range []string{b.SourceManifestSHA256, b.ContextSHA256, b.CollectorSHA256, b.CatalogContractSHA256} {
		if !c18CatalogHashValid(s, 32) {
			return fmt.Errorf("binding pin")
		}
	}
	if len(b.MigrationSHA256) != 18 {
		return fmt.Errorf("binding migration set")
	}
	for n, h := range b.MigrationSHA256 {
		if !c18CatalogHashValid(h, 32) || filepath.Base(n) != n {
			return fmt.Errorf("binding migration hash")
		}
	}
	return nil
}
func c18CatalogMigrationHashes() (map[string]string, error) {
	names, e := fs.Glob(migrationsFS, "migrations/*.sql")
	if e != nil {
		return nil, e
	}
	disk, e := filepath.Glob("migrations/*.sql")
	if e != nil {
		return nil, e
	}
	if len(names) != 18 || !reflect.DeepEqual(names, disk) {
		return nil, fmt.Errorf("unexpected migration files")
	}
	out := map[string]string{}
	for i, n := range names {
		base := filepath.Base(n)
		prefix, _, ok := strings.Cut(base, "_")
		v, e := strconv.Atoi(prefix)
		if !ok || e != nil || v != i+1 {
			return nil, fmt.Errorf("unexpected migration version")
		}
		b, e := migrationsFS.ReadFile(n)
		if e != nil {
			return nil, e
		}
		raw, e := os.ReadFile(n)
		if e != nil {
			return nil, e
		}
		if !bytes.Equal(b, raw) {
			return nil, fmt.Errorf("migration disk/embed mismatch: %s", base)
		}
		out[base] = c18CatalogSHA(b)
	}
	return out, nil
}

func c18CatalogNormalize(c c18Catalog) error {
	if len(c) != len(c18CatalogQueries) {
		return fmt.Errorf("catalog collection set")
	}
	for _, q := range c18CatalogQueries {
		rows, ok := c[q.Name]
		if !ok || rows == nil {
			return fmt.Errorf("catalog collection missing: %s", q.Name)
		}
		keys := strings.Fields(q.Keys)
		for _, r := range rows {
			if len(r) != len(keys) {
				return fmt.Errorf("catalog field set: %s", q.Name)
			}
			for _, k := range keys {
				if _, ok := r[k]; !ok {
					return fmt.Errorf("catalog field missing: %s.%s", q.Name, k)
				}
			}
		}
		type item struct {
			r c18CatalogRow
			b string
		}
		items := make([]item, 0, len(rows))
		for _, r := range rows {
			b, e := c18CatalogBytes(r)
			if e != nil {
				return e
			}
			items = append(items, item{r, string(b)})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].b < items[j].b })
		for i, v := range items {
			if i > 0 && v.b == items[i-1].b {
				return fmt.Errorf("duplicate catalog row: %s", q.Name)
			}
			rows[i] = v.r
		}
	}
	return nil
}
func c18CatalogCompare(reference, observed c18Catalog) error {
	if e := c18CatalogNormalize(reference); e != nil {
		return e
	}
	if e := c18CatalogNormalize(observed); e != nil {
		return e
	}
	for _, q := range c18CatalogQueries {
		a, e := c18CatalogBytes(reference[q.Name])
		if e != nil {
			return e
		}
		b, e := c18CatalogBytes(observed[q.Name])
		if e != nil {
			return e
		}
		if !bytes.Equal(a, b) {
			return fmt.Errorf("catalog drift: %s", q.Name)
		}
	}
	return nil
}
func c18CatalogValidateLocalState(c c18Catalog) error {
	if e := c18CatalogNormalize(c); e != nil {
		return e
	}
	for section, keys := range map[string]string{
		"schemas": "name owner", "tables": "table kind persistence owner rls forced_rls",
		"columns":     "table name position type formatted_type typmod nullable identity generated dropped",
		"constraints": "name type definition validated deferrable initially_deferred",
		"indexes":     "table name definition valid ready live", "triggers": "table name function definition enabled deferrable initially_deferred type_bits internal",
		"sequences": "name type start min max increment cycle cache owner persistence",
		"functions": "schema name identity_arguments return_type language definition security_definer volatility strict parallel leakproof owner kind",
		"types":     "name kind category owner defined", "enums": "type label order", "extensions": "name schema version owner relocatable",
		"dependencies": "object_type object reference_type reference kind",
	} {
		for _, r := range c[section] {
			for _, key := range strings.Fields(keys) {
				if r[key] == nil {
					return fmt.Errorf("unknown catalog value: %s.%s", section, key)
				}
			}
		}
	}
	for _, section := range []string{"schemas", "tables", "columns", "constraints", "indexes", "triggers", "sequences", "functions", "types", "enums", "dependencies"} {
		if len(c[section]) == 0 {
			return fmt.Errorf("empty required catalog: %s", section)
		}
	}
	if len(c["goose"]) != 19 {
		return fmt.Errorf("unexpected Goose history")
	}
	versions := map[int]bool{}
	for _, r := range c["goose"] {
		n, ok := r["version"].(json.Number)
		if !ok {
			return fmt.Errorf("Goose version type")
		}
		v, e := strconv.Atoi(string(n))
		id, ok := r["id"].(json.Number)
		if e != nil || !ok || v < 0 || v > 18 || versions[v] || r["applied"] != true || string(id) != strconv.Itoa(v+1) {
			return fmt.Errorf("unexpected Goose history")
		}
		versions[v] = true
	}
	for _, r := range c["indexes"] {
		if r["valid"] != true || r["ready"] != true || r["live"] != true {
			return fmt.Errorf("unsafe index state")
		}
	}
	for _, r := range c["triggers"] {
		if r["enabled"] != "O" {
			return fmt.Errorf("unsafe trigger state")
		}
	}
	for _, r := range c["constraints"] {
		if r["validated"] != true {
			return fmt.Errorf("unvalidated constraint")
		}
	}
	if e := c18CatalogValidateSupplemental(c); e != nil {
		return e
	}
	extensions := map[string]bool{}
	for _, r := range c["extensions"] {
		s, ok := r["name"].(string)
		if !ok {
			return fmt.Errorf("extension identity")
		}
		extensions[s] = true
	}
	if len(extensions) != 3 || !extensions["plpgsql"] || !extensions["pgcrypto"] || !extensions["pg_trgm"] {
		return fmt.Errorf("unexpected extensions")
	}
	return nil
}

const c18CatalogDependencyClasses = "pg_namespace pg_class pg_type pg_proc pg_constraint pg_attrdef pg_trigger pg_rewrite pg_policy pg_operator pg_opclass pg_opfamily pg_amop pg_amproc pg_collation pg_default_acl pg_extension pg_language pg_am pg_tablespace"

func c18CatalogExactKeys(row map[string]any, keys string) bool {
	names := strings.Fields(keys)
	if len(row) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := row[name]; !ok {
			return false
		}
	}
	return true
}
func c18CatalogValidateSupplemental(c c18Catalog) error {
	allowed := map[string]bool{}
	for _, name := range strings.Fields(c18CatalogDependencyClasses) {
		allowed[name] = true
	}
	for _, r := range c["dependencies"] {
		for _, key := range []string{"object_class", "reference_class"} {
			v, ok := r[key].(string)
			if !ok || !allowed[v] {
				return fmt.Errorf("unsupported dependency class: %s", key)
			}
		}
	}
	for _, r := range c["dependencies"] {
		for _, side := range []string{"object", "reference"} {
			if r[side+"_resolved"] != true {
				return fmt.Errorf("unresolved dependency owner: %s", side)
			}
			schema := r[side+"_schema"]
			if schema == nil {
				switch r[side+"_class"] {
				case "pg_extension", "pg_language", "pg_am", "pg_tablespace", "pg_default_acl":
				default:
					return fmt.Errorf("unresolved dependency owner: %s", side)
				}
			} else if schema != "public" && schema != "pg_catalog" {
				other := "reference"
				if side == "reference" {
					other = "object"
				}
				ownedToast := schema == "pg_toast" && r[side+"_in_scope"] == true
				// Namespace membership of an address-verified owned TOAST relation is a
				// narrowly justified system edge, not a pg_toast/schema-wide exemption.
				ownedToastNamespace := schema == "pg_toast" && r[side+"_class"] == "pg_namespace" && r[other+"_class"] == "pg_class" && r[other+"_schema"] == "pg_toast" && r[other+"_in_scope"] == true
				if !ownedToast && !ownedToastNamespace {
					return fmt.Errorf("unsupported dependency owner schema: %s", side)
				}
			}
		}
	}
	for _, r := range c["comments"] {
		v, ok := r["class"].(string)
		if !ok || !allowed[v] {
			return fmt.Errorf("unsupported comment class")
		}
	}
	for _, r := range c["toast"] {
		if r["options"] != nil {
			return fmt.Errorf("unsupported TOAST options")
		}
		columns, ok := r["columns"].([]any)
		if !ok || len(columns) != 3 {
			return fmt.Errorf("unsupported TOAST column shape")
		}
		for _, v := range columns {
			col, ok := v.(map[string]any)
			if !ok || !c18CatalogExactKeys(col, "name position type typmod not_null storage compression statistics options acl") {
				return fmt.Errorf("unknown TOAST column definition")
			}
		}
		indexes, ok := r["indexes"].([]any)
		if !ok || len(indexes) != 1 {
			return fmt.Errorf("unsupported TOAST index shape")
		}
		for _, v := range indexes {
			idx, ok := v.(map[string]any)
			if !ok || !c18CatalogExactKeys(idx, "definition valid ready live unique options tablespace access_method") || idx["valid"] != true || idx["ready"] != true || idx["live"] != true || idx["unique"] != true || idx["options"] != nil {
				return fmt.Errorf("unsafe TOAST index state")
			}
		}
	}
	for _, r := range c["db_role_settings"] {
		name, ok := r["name"].(string)
		if !ok || name == "" || r["known"] != true {
			return fmt.Errorf("unknown persistent setting")
		}
		// Explicit supported persistent settings. Everything else, including custom
		// GUCs, planner/durability/preload overrides, requires a new source review.
		switch strings.ToLower(name) {
		case "lock_timeout", "statement_timeout", "idle_in_transaction_session_timeout", "idle_session_timeout", "search_path", "timezone", "datestyle", "client_min_messages":
		default:
			return fmt.Errorf("unsupported persistent setting: %s", name)
		}
		if _, ok := r["value"].(string); !ok {
			return fmt.Errorf("unknown persistent setting value")
		}
	}
	return nil
}

func c18CatalogValidateSettings(settings map[string]c18CatalogSetting) error {
	for k, v := range map[string]string{"fsync": "on", "full_page_writes": "on", "synchronous_commit": "on", "autovacuum": "on", "default_table_access_method": "heap", "default_tablespace": "", "session_replication_role": "origin", "row_security": "on", "default_transaction_read_only": "off", "track_counts": "on", "shared_preload_libraries": "", "session_preload_libraries": "", "local_preload_libraries": ""} {
		s, ok := settings[k]
		if !ok || s.Value != v || s.Pending {
			return fmt.Errorf("unsafe/missing PostgreSQL setting: %s", k)
		}
	}
	if s, ok := settings["wal_level"]; !ok || (s.Value != "replica" && s.Value != "logical") {
		return fmt.Errorf("unsafe/missing PostgreSQL setting: wal_level")
	}
	for k, s := range settings {
		if strings.HasPrefix(k, "enable_") && s.Value != s.Boot {
			return fmt.Errorf("planner override: %s", k)
		}
		if s.Pending {
			return fmt.Errorf("pending restart setting: %s", k)
		}
	}
	return nil
}

const c18CatalogScopeKeys = "foreign_schema foreign_relations unsupported_functions unsupported_types extensions event_triggers foreign_servers foreign_wrappers publications subscriptions largeobjects security_labels transforms extended_statistics conversions text_search casts custom_access_methods custom_languages analyzed_public_tables"

func c18CatalogValidateScope(scope map[string]json.Number) error {
	keys := strings.Fields(c18CatalogScopeKeys)
	if len(scope) != len(keys) {
		return fmt.Errorf("scope coverage changed")
	}
	for _, k := range keys {
		n, ok := scope[k]
		if !ok {
			return fmt.Errorf("scope coverage changed")
		}
		if n != "0" {
			return fmt.Errorf("unsupported catalog scope: %s", k)
		}
	}
	return nil
}

type c18CatalogQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func c18CatalogCollect(ctx context.Context, q c18CatalogQuerier, b c18CatalogBinding, bindingSHA, datname, stage string) (c18CatalogCapture, error) {
	out := c18CatalogCapture{Format: c18CatalogFormat, Canonical: c18CatalogCanonical, Binding: b, BindingSHA256: bindingSHA, Datname: datname, Stage: stage, Maintenance: c18CatalogMaintenance, Settings: map[string]c18CatalogSetting{}, Catalog: c18Catalog{}}
	if e := c18CatalogValidateBinding(b); e != nil {
		return out, e
	}
	bound, e := c18CatalogBytes(b)
	if e != nil || c18CatalogSHA(bound) != bindingSHA {
		return out, fmt.Errorf("capture binding digest mismatch")
	}
	if stage != "official_new_pre_population" && stage != "official_store_reopen_pre_population" && stage != "owned_transaction_negative_control" {
		return out, fmt.Errorf("unknown catalog stage")
	}
	var actual, raw string
	if e := q.QueryRow(ctx, `SELECT current_database(),current_setting('server_version'),current_setting('server_version_num')::int`).Scan(&actual, &out.ServerVersion, &out.ServerVersionNum); e != nil {
		return out, e
	}
	if actual != datname || !strings.HasPrefix(datname, "tm_c18_catalog_") {
		return out, fmt.Errorf("unowned database")
	}
	if out.ServerVersionNum < 160000 || out.ServerVersionNum >= 170000 {
		return out, fmt.Errorf("unsupported PostgreSQL catalog major: %d", out.ServerVersionNum)
	}
	if e := q.QueryRow(ctx, c18CatalogScopeSQL).Scan(&raw); e != nil {
		return out, e
	}
	var scope map[string]json.Number
	if e := c18CatalogDecode([]byte(raw), &scope); e != nil {
		return out, e
	}
	if e = c18CatalogValidateScope(scope); e != nil {
		return out, e
	}
	rows, e := q.Query(ctx, c18CatalogSettingsSQL)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var name string
		var s c18CatalogSetting
		if e = rows.Scan(&name, &s.Value, &s.Unit, &s.Source, &s.Boot, &s.Reset, &s.Pending); e != nil {
			rows.Close()
			return out, e
		}
		out.Settings[name] = s
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if e = c18CatalogValidateSettings(out.Settings); e != nil {
		return out, e
	}
	if e = q.QueryRow(ctx, `SELECT jsonb_build_object('owner',pg_get_userbyid(datdba),'encoding',pg_encoding_to_char(encoding),'locale_provider',datlocprovider,'collate',datcollate,'ctype',datctype,'icu_locale',daticulocale,'icu_rules',daticurules,'collation_version',datcollversion,'acl',datacl::text,'connection_limit',datconnlimit,'allow_connections',datallowconn,'tablespace',t.spcname)::text FROM pg_database d JOIN pg_tablespace t ON t.oid=d.dattablespace WHERE datname=current_database()`).Scan(&raw); e != nil {
		return out, e
	}
	if e = c18CatalogDecode([]byte(raw), &out.Database); e != nil {
		return out, e
	}
	for _, spec := range c18CatalogQueries {
		rows, e = q.Query(ctx, spec.SQL)
		if e != nil {
			return out, fmt.Errorf("catalog query %s: %w", spec.Name, e)
		}
		out.Catalog[spec.Name] = []c18CatalogRow{}
		for rows.Next() {
			var s string
			if e = rows.Scan(&s); e != nil {
				rows.Close()
				return out, e
			}
			var row c18CatalogRow
			if e = c18CatalogDecode([]byte(s), &row); e != nil {
				rows.Close()
				return out, e
			}
			if len(out.Catalog[spec.Name]) >= 10000 || len(s) > 1<<20 {
				rows.Close()
				return out, fmt.Errorf("catalog resource ceiling: %s", spec.Name)
			}
			out.Catalog[spec.Name] = append(out.Catalog[spec.Name], row)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	if e = c18CatalogValidateLocalState(out.Catalog); e != nil {
		return out, e
	}
	if e = c18CatalogCollectExternal(ctx, q, b.external, out.Catalog); e != nil {
		return out, e
	}
	if e = c18CatalogValidateState(out.Catalog); e != nil {
		return out, e
	}
	out.EmbeddedMigrationSHA256, e = c18CatalogMigrationHashes()
	if e != nil {
		return out, e
	}
	if !reflect.DeepEqual(out.EmbeddedMigrationSHA256, b.MigrationSHA256) {
		return out, fmt.Errorf("migration binding mismatch")
	}
	rawCatalog, e := c18CatalogBytes(out.Catalog)
	if e != nil {
		return out, e
	}
	out.CatalogSHA256 = c18CatalogSHA(rawCatalog)
	return out, nil
}

// A short-lived read-only repeatable-read snapshot; no ALTER, ANALYZE, VACUUM,
// planner, durability, pool or workload tuning. SET LOCAL is deparser context.
func c18CatalogSnapshot(ctx context.Context, st *PgStore, b c18CatalogBinding, pin, name, stage string) (c18CatalogCapture, error) {
	tx, e := st.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return c18CatalogCapture{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SET LOCAL search_path = pg_catalog, public`); e != nil {
		return c18CatalogCapture{}, e
	}
	c, e := c18CatalogCollect(ctx, tx, b, pin, name, stage)
	if e != nil {
		return c, e
	}
	return c, tx.Commit(ctx)
}

// The random name/DSN/source guards below are not a native fixture owner receipt.
// C18-09 and the sole runner must independently attest that runtime ownership.
// Fresh TEMPLATE template0 only; never a schema16 or previously migrated image.
// The admin connection creates/drops the exact owned name and, only in the
// explicit SQL coverage case, SETs/RESETs lock_timeout on that owned database.
// No cleanup callback is acquired until CREATE succeeds. No parallel tests.
type c18CatalogOwned struct {
	admin   c18CatalogAdmin
	store   *PgStore
	cfg     config.DB
	name    string
	created bool
}

type c18CatalogAdmin interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Close(context.Context) error
}

func (o *c18CatalogOwned) close(ctx context.Context) (result error) {
	if o.store != nil {
		_ = o.store.Close()
		o.store = nil
	}
	if o.admin == nil {
		if o.created {
			return fmt.Errorf("owned database residual or cleanup unknown: %s", o.name)
		}
		return nil
	}
	admin := o.admin
	defer func() {
		// Even DROP failure/canceled cleanup must close this session. The source-only
		// ownership guard is not external native-fixture ownership attestation.
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		closeErr := admin.Close(closeCtx)
		o.admin = nil
		if closeErr != nil {
			result = errors.Join(result, fmt.Errorf("admin close failed: %T", closeErr))
		}
		if o.created {
			result = errors.Join(result, fmt.Errorf("owned database residual or cleanup unknown: %s", o.name))
		}
	}()
	if o.created {
		if _, e := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{o.name}.Sanitize()+" WITH (FORCE)"); e != nil {
			return fmt.Errorf("owned DROP failed: %T", e)
		}
		var exists bool
		if e := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)", o.name).Scan(&exists); e != nil {
			return fmt.Errorf("owned DROP verification failed: %T", e)
		}
		if exists {
			return fmt.Errorf("owned database remains after DROP")
		}
		o.created = false
	}
	return nil
}
func c18CatalogOwn(t *testing.T, ctx context.Context, dsn string, owner c18CatalogOwnerReceipt) *c18CatalogOwned {
	t.Helper()
	u, e := url.Parse(dsn)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		t.Fatal("qualification requires explicit PostgreSQL URL")
	}
	cfg, e := pgx.ParseConfig(dsn)
	if e != nil {
		t.Fatalf("qualification DSN parse: %T", e)
	}
	if len(cfg.RuntimeParams) != 0 {
		t.Fatal("qualification DSN must not override runtime settings")
	}
	if cfg.Host != owner.Payload.Host || cfg.Port != owner.Payload.Port || cfg.Database != owner.Payload.AdminDatabase || cfg.User != owner.Payload.Role || time.Now().Unix() >= owner.Payload.ExpiresUnixSeconds {
		t.Fatal("signed fixture owner endpoint/role/deadline mismatch")
	}
	cfg.ConnectTimeout = 5 * time.Second
	admin, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatalf("qualification admin connect: %T", e)
	}
	o := &c18CatalogOwned{admin: admin}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if e := o.close(cleanup); e != nil {
			t.Errorf("owned catalog cleanup failed: %v", e)
		}
	})
	// Read-only native identity check precedes CREATE, role DDL, or system-schema
	// fixture probes. A bare env flag and an arbitrary superuser DSN are insufficient.
	var systemID, actualRole string
	var postmasterStart int64
	var super bool
	e = admin.QueryRow(ctx, `SELECT system_identifier::text,(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint,current_user,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()`).Scan(&systemID, &postmasterStart, &actualRole, &super)
	if e != nil || systemID != owner.Payload.SystemIdentifier || postmasterStart != owner.Payload.PostmasterStartUnixMicro || actualRole != owner.Payload.Role || !super {
		t.Fatal("actual fixture cluster does not match signed external owner/C18-09 receipt")
	}
	var token [16]byte
	if _, e = rand.Read(token[:]); e != nil {
		t.Fatal(e)
	}
	o.name = "tm_c18_catalog_" + hex.EncodeToString(token[:])
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{o.name}.Sanitize()+" TEMPLATE template0"); e != nil {
		t.Fatalf("qualification CREATE database: %T", e)
	}
	o.created = true
	cfg.Database = o.name
	u.Path = "/" + o.name
	u.RawPath = ""
	// pgx.ConnConfig.ConnString returns the ORIGINAL DSN. Never use it after
	// mutating Database: doing so would migrate the admin database.
	query := u.Query()
	query.Del("dbname")
	query.Del("database")
	u.RawQuery = query.Encode()
	verified, e := pgx.ParseConfig(u.String())
	if e != nil || verified.Database != o.name {
		t.Fatal("owned DSN database identity mismatch")
	}
	o.cfg = config.DB{DSN: u.String(), MaxOpenConns: 2, MaxIdleConns: 0, ConnMaxLifetime: time.Minute}
	probe, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatalf("owned pristine connection: %T", e)
	}
	var actual string
	var objects, extensions, schemas int
	e = probe.QueryRow(ctx, `SELECT current_database(),(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace)+(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace)+(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace)+(SELECT count(*) FROM pg_default_acl)+(SELECT count(*) FROM pg_depend d CROSS JOIN LATERAL pg_identify_object(d.classid,d.objid,d.objsubid) x WHERE x.schema='public'),(SELECT count(*) FROM pg_extension WHERE extname<>'plpgsql'),(SELECT count(*) FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%' AND nspname NOT LIKE 'pg_temp_%')`).Scan(&actual, &objects, &extensions, &schemas)
	var scopeRaw string
	var scope map[string]json.Number
	var safe bool
	if e == nil {
		e = probe.QueryRow(ctx, c18CatalogScopeSQL).Scan(&scopeRaw)
	}
	if e == nil {
		e = c18CatalogDecode([]byte(scopeRaw), &scope)
	}
	if e == nil {
		e = c18CatalogValidateScope(scope)
	}
	if e == nil {
		e = probe.QueryRow(ctx, `SELECT current_setting('server_version_num')::int BETWEEN 160000 AND 169999 AND current_setting('fsync')='on' AND current_setting('full_page_writes')='on' AND current_setting('synchronous_commit')='on'`).Scan(&safe)
	}
	_ = probe.Close(ctx)
	if e != nil || !safe || actual != o.name || objects != 0 || extensions != 0 || schemas != 0 {
		t.Fatal("owned template0 is not pristine; official migration not invoked")
	}
	// Official New owns official Migrate; no copied schema or SQL fixture.
	o.store, e = New(ctx, o.cfg)
	if e != nil {
		t.Fatalf("qualification official New/Migrate: %T", e)
	}
	return o
}

func c18CatalogInput(t *testing.T) (c18CatalogBinding, string, string) {
	t.Helper()
	if os.Getenv("TABMAIL_C18_CATALOG_ROLE_PROBES") != "transaction_owned_roles" {
		t.Fatal("separate transaction-owned role probe authorization required")
	}
	if os.Getenv("TABMAIL_C18_CATALOG_QUALIFICATION") != "owned_reference_candidate_only" {
		t.Fatal("separate owned C18-07 qualification authorization required; not a benchmark selector")
	}
	path, pin := os.Getenv("TABMAIL_C18_CATALOG_BINDING"), os.Getenv("TABMAIL_C18_CATALOG_BINDING_SHA256")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatalf("binding input: %T", e)
	}
	if !c18CatalogHashValid(pin, 32) || c18CatalogSHA(raw) != pin {
		t.Fatal("external binding pin mismatch")
	}
	var b c18CatalogBinding
	if e = c18CatalogDecode(raw, &b); e != nil {
		t.Fatal("binding grammar")
	}
	canonical, e := c18CatalogBytes(b)
	if e != nil || !bytes.Equal(canonical, raw) {
		t.Fatal("binding must be exact canonical bytes (duplicates forbidden)")
	}
	if e = c18CatalogValidateBinding(b); e != nil {
		t.Fatal(e)
	}
	for name, want := range map[string]string{"r5_current_benchmark_catalog_test.go": b.CollectorSHA256, "../../../scripts/contracts/r5-benchmark-current18-catalog-v1.json": b.CatalogContractSHA256} {
		r, e := os.ReadFile(name)
		if e != nil || c18CatalogSHA(r) != want {
			t.Fatalf("qualification source pin mismatch: %s", name)
		}
	}
	hashes, e := c18CatalogMigrationHashes()
	if e != nil || !reflect.DeepEqual(hashes, b.MigrationSHA256) {
		t.Fatal("qualification migration pin mismatch")
	}
	dsn := os.Getenv("TABMAIL_C18_CATALOG_ADMIN_DSN")
	if dsn == "" {
		t.Fatal("dedicated owned qualification admin DSN required")
	}
	return b, pin, dsn
}

// Exact selector, deliberately not TestR5SchemaInventoryAndRestart. Produces an
// unsigned candidate only AFTER native probes and verified owned-DB cleanup.
func TestR5Current18FullCatalogOwnedQualification(t *testing.T) {
	binding, pin, dsn := c18CatalogInput(t)
	owner, ownerSHA := c18CatalogLoadOwner(t, binding)
	binding.external = c18CatalogLoadExternalReference(t, owner)
	deadline := time.Now().Add(150 * time.Second)
	if ownerExpiry := time.Unix(owner.Payload.ExpiresUnixSeconds, 0); ownerExpiry.Before(deadline) {
		deadline = ownerExpiry
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	first := c18CatalogOwn(t, ctx, dsn, owner)
	initial, e := c18CatalogSnapshot(ctx, first.store, binding, pin, first.name, "official_new_pre_population")
	if e != nil {
		t.Fatal(e)
	}
	_ = first.store.Close()
	first.store = nil
	first.store, e = New(ctx, first.cfg)
	if e != nil {
		t.Fatalf("official store reopen New: %T", e)
	}
	cfg, e := pgx.ParseConfig(first.cfg.DSN)
	if e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, cfg); e != nil {
		t.Fatalf("official store reopen Migrate: %T", e)
	}
	restarted, e := c18CatalogSnapshot(ctx, first.store, binding, pin, first.name, "official_store_reopen_pre_population")
	if e != nil {
		t.Fatal(e)
	}
	if e = c18CatalogCompare(initial.Catalog, restarted.Catalog); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(initial.Settings, restarted.Settings) || !reflect.DeepEqual(initial.Database, restarted.Database) {
		t.Fatal("store reopen PostgreSQL context drift")
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 15*time.Second)
	e = first.close(cleanup)
	cancelCleanup()
	if e != nil {
		t.Fatalf("first owned database cleanup: %v", e)
	}
	second := c18CatalogOwn(t, ctx, dsn, owner)
	fresh, e := c18CatalogSnapshot(ctx, second.store, binding, pin, second.name, "official_new_pre_population")
	if e != nil {
		t.Fatal(e)
	}
	if e = c18CatalogCompare(initial.Catalog, fresh.Catalog); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(initial.Settings, fresh.Settings) || !reflect.DeepEqual(initial.Database, fresh.Database) {
		t.Fatal("independent fresh PostgreSQL context drift")
	}
	ledger := c18CatalogNewLedger()
	c18CatalogNativeDrift(t, ctx, second, binding, pin, fresh.Catalog, ledger)
	c18CatalogOwnedSQLCoverage(t, ctx, second, binding, pin, fresh.Catalog, ledger)
	if e = ledger.validate(); e != nil {
		t.Fatal(e)
	}
	cleanup, cancelCleanup = context.WithTimeout(context.Background(), 15*time.Second)
	e = second.close(cleanup)
	cancelCleanup()
	if e != nil {
		t.Fatalf("second owned database cleanup: %v", e)
	}
	// Recheck the exact collector/contracts/migrations after the owned work. The
	// source manifest/context pins remain external claims, not a C18-09 receipt.
	after, afterPin, _ := c18CatalogInput(t)
	after.external = binding.external
	if !reflect.DeepEqual(binding, after) || afterPin != pin {
		t.Fatal("qualification source binding drift")
	}
	ownerAfter, ownerAfterSHA := c18CatalogLoadOwner(t, binding)
	externalAfter := c18CatalogLoadExternalReference(t, ownerAfter)
	if !reflect.DeepEqual(externalAfter, binding.external) {
		t.Fatal("external provenance reference drift")
	}
	if ownerAfterSHA != ownerSHA || !reflect.DeepEqual(ownerAfter, owner) {
		t.Fatal("external fixture owner receipt drift")
	}
	payload := map[string]any{"fixture_owner_receipt_sha256": ownerSHA, "external_reference_sha256": owner.Payload.ExternalReferenceSHA256, "required_bootstrap_profile": binding.external.Profile, "kind": "unsigned_current18_catalog_source_v5_candidate", "full_semantic_reference_qualified": false, "runtime_reference_ready": false, "source_manifest_independently_verified": false, "runtime_qualification": false, "benchmark_run": false, "captures": []c18CatalogCapture{initial, restarted, fresh}, "restart_kind": "store_reopen", "native_restart_performed": false, "native_fixture_ownership_independently_verified": false, "store_reopen_catalog_unchanged": true, "independent_clean_catalog_unchanged": true, "owned_sql_source_cases_completed": len(ledger.completedList()) == len(ledger.requiredList()), "required_owned_cases": ledger.requiredList(), "completed_owned_cases": ledger.completedList(), "no_native_server_restart_proof": true, "owned_databases_dropped": []string{first.name, second.name}, "cleanup_verified": true}
	raw, e := c18CatalogBytes(payload)
	if e != nil {
		t.Fatal(e)
	}
	result := map[string]any{"candidate": json.RawMessage(raw), "candidate_sha256": c18CatalogSHA(raw)}
	raw, e = c18CatalogBytes(result)
	if e != nil {
		t.Fatal(e)
	}
	if len(raw) > 16<<20 {
		t.Fatal("catalog candidate exceeds 16 MiB resource ceiling")
	}
	output := os.Getenv("TABMAIL_C18_CATALOG_OUTPUT")
	if output == "" {
		t.Fatal("new exclusive candidate output path required")
	}
	f, e := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatalf("exclusive candidate output: %T", e)
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		t.Fatal("candidate output incomplete; not qualified")
	}
	t.Log("unsigned catalog candidate written; requires independent reader/evi signing; no runtime reference activation")
}

func c18CatalogNativeDrift(t *testing.T, ctx context.Context, o *c18CatalogOwned, b c18CatalogBinding, pin string, reference c18Catalog, ledger *c18CatalogLedger) {
	t.Helper()
	probes := []struct{ name, sql, want string }{
		{"varchar255_to_1", `ALTER TABLE public.tenants ALTER COLUMN name TYPE varchar(1) USING left(name,1)`, "catalog drift: columns"},
		{"trigger_disabled", `ALTER TABLE public.permission_profiles DISABLE TRIGGER permission_profile_revision`, "unsafe trigger state"},
		{"sequence_cache", `ALTER SEQUENCE public.permission_editor_revision_seq CACHE 9`, "catalog drift: sequences"},
		{"sequence_ownership", `ALTER SEQUENCE public.permission_editor_revision_seq OWNED BY public.users.permission_revision`, "catalog drift: sequences"},
		{"function_definition", `CREATE OR REPLACE FUNCTION public.permission_profile_revision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`, "catalog drift: functions"},
		{"outside_schema", `CREATE SCHEMA c18_catalog_disallowed`, "unsupported catalog scope: foreign_schema"},
	}
	for _, probe := range probes {
		c18CatalogRunCase(t, ledger, probe.name, func(t *testing.T) {
			tx, e := o.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				rollback, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if e := tx.Rollback(rollback); e != nil && e != pgx.ErrTxClosed {
					t.Errorf("owned probe rollback: %T", e)
				}
			}()
			if _, e = tx.Exec(ctx, `SET LOCAL search_path = pg_catalog, public`); e != nil {
				t.Fatal(e)
			}
			positive, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
			if e != nil {
				t.Fatal(e)
			}
			if e = c18CatalogCompare(reference, positive.Catalog); e != nil {
				t.Fatal(e)
			}
			if _, e = tx.Exec(ctx, probe.sql); e != nil {
				t.Fatalf("native drift mutation %s: %T", probe.name, e)
			}
			changed, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
			if e == nil {
				e = c18CatalogCompare(reference, changed.Catalog)
			}
			if e == nil || e.Error() != probe.want {
				t.Fatalf("native rejection gate: got %v, want %s", e, probe.want)
			}
		})
	}
	if t.Failed() {
		t.Fatal("native catalog qualification failed; no candidate")
	}
	final, e := c18CatalogSnapshot(ctx, o.store, b, pin, o.name, "official_new_pre_population")
	if e != nil {
		t.Fatal(e)
	}
	if e = c18CatalogCompare(reference, final.Catalog); e != nil {
		t.Fatal("native probe rollback catalog mismatch:", e)
	}
}

// Pure controls exercise the very comparator used above. They never invoke a
// connection, subprocess, benchmark, legacy inventory or actual reference.
func c18CatalogSynthetic() c18Catalog {
	c := c18Catalog{}
	for _, q := range c18CatalogQueries {
		r := c18CatalogRow{}
		for _, k := range strings.Fields(q.Keys) {
			r[k] = nil
		}
		c[q.Name] = []c18CatalogRow{r}
	}
	c["columns"][0]["formatted_type"] = "character varying(255)"
	c["columns"][0]["typmod"] = json.Number("259")
	c["indexes"][0]["valid"] = true
	c["indexes"][0]["ready"] = true
	c["triggers"][0]["enabled"] = "O"
	c["sequences"][0]["cache"] = json.Number("1")
	c["functions"][0]["definition"] = "synthetic original function body"
	return c
}
func TestR5Current18FullCatalogSyntheticDrift(t *testing.T) {
	for _, p := range []struct {
		name, section, key string
		value              any
	}{
		{"varchar255_to_1", "columns", "formatted_type", "character varying(1)"},
		{"typmod255_to_1", "columns", "typmod", json.Number("5")},
		{"trigger_disabled", "triggers", "enabled", "D"},
		{"index_invalid", "indexes", "valid", false},
		{"index_not_ready", "indexes", "ready", false},
		{"sequence_cache", "sequences", "cache", json.Number("9")},
		{"sequence_owner_table", "sequences", "owner_table", "users"},
		{"sequence_owner_column", "sequences", "owner_column", "permission_revision"},
		{"sequence_ownership_kind", "sequences", "ownership_dependency_kind", "a"},
		{"function_definition", "functions", "definition", "synthetic changed function body"},
		{"ordinary_function_security", "functions", "security_definer", true},
		{"enum_label", "enums", "label", "different"},
		{"domain_base", "types", "base", "smallint"},
		{"acl", "tables", "acl", "{PUBLIC=arwdDxt/postgres}"},
		{"owner", "tables", "owner", "another_role"},
		{"dependency", "dependencies", "reference", "other_schema.object"},
	} {
		t.Run(p.name, func(t *testing.T) {
			a, b := c18CatalogSynthetic(), c18CatalogSynthetic()
			if e := c18CatalogCompare(a, b); e != nil {
				t.Fatal("same-source positive:", e)
			}
			b[p.section][0][p.key] = p.value
			if e := c18CatalogCompare(a, b); e == nil || e.Error() != "catalog drift: "+p.section {
				t.Fatalf("wrong drift gate: %v", e)
			}
		})
	}
}
func TestR5Current18FullCatalogShapeFailClosed(t *testing.T) {
	for _, q := range c18CatalogQueries {
		t.Run(q.Name, func(t *testing.T) {
			a, b := c18CatalogSynthetic(), c18CatalogSynthetic()
			if e := c18CatalogCompare(a, b); e != nil {
				t.Fatal(e)
			}
			delete(b, q.Name)
			if e := c18CatalogCompare(a, b); e == nil || e.Error() != "catalog collection set" {
				t.Fatalf("missing collection gate: %v", e)
			}
			for _, key := range strings.Fields(q.Keys) {
				b = c18CatalogSynthetic()
				delete(b[q.Name][0], key)
				if e := c18CatalogCompare(a, b); e == nil || e.Error() != "catalog field set: "+q.Name {
					t.Fatalf("missing %s gate: %v", key, e)
				}
			}
			b = c18CatalogSynthetic()
			b[q.Name][0]["unexpected"] = true
			if e := c18CatalogCompare(a, b); e == nil || e.Error() != "catalog field set: "+q.Name {
				t.Fatalf("unknown field gate: %v", e)
			}
		})
	}
}
func TestR5Current18FullCatalogCanonicalOrder(t *testing.T) {
	a, b := c18CatalogSynthetic(), c18CatalogSynthetic()
	row := c18CatalogSynthetic()["tables"][0]
	row["table"] = "second"
	a["tables"] = append(a["tables"], row)
	b["tables"] = append([]c18CatalogRow{row}, b["tables"]...)
	if e := c18CatalogCompare(a, b); e != nil {
		t.Fatal(e)
	}
	ar, e := c18CatalogBytes(a)
	if e != nil {
		t.Fatal(e)
	}
	br, e := c18CatalogBytes(b)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(ar, br) {
		t.Fatal("canonical order differs")
	}
	b["tables"] = append(b["tables"], row)
	if e := c18CatalogNormalize(b); e == nil || e.Error() != "duplicate catalog row: tables" {
		t.Fatalf("duplicate gate: %v", e)
	}
}
func TestR5Current18FullCatalogUnsafeSettings(t *testing.T) {
	good := map[string]c18CatalogSetting{}
	for k, v := range map[string]string{"fsync": "on", "full_page_writes": "on", "synchronous_commit": "on", "autovacuum": "on", "default_table_access_method": "heap", "default_tablespace": "", "session_replication_role": "origin", "row_security": "on", "default_transaction_read_only": "off", "track_counts": "on", "shared_preload_libraries": "", "session_preload_libraries": "", "local_preload_libraries": "", "wal_level": "replica", "enable_seqscan": "on"} {
		good[k] = c18CatalogSetting{Value: v, Boot: v}
	}
	if e := c18CatalogValidateSettings(good); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"fsync", "full_page_writes", "synchronous_commit", "autovacuum", "row_security", "track_counts"} {
		t.Run(key, func(t *testing.T) {
			saved := good[key]
			bad := saved
			bad.Value = "off"
			good[key] = bad
			if e := c18CatalogValidateSettings(good); e == nil || e.Error() != "unsafe/missing PostgreSQL setting: "+key {
				t.Fatalf("unsafe gate: %v", e)
			}
			delete(good, key)
			if e := c18CatalogValidateSettings(good); e == nil || e.Error() != "unsafe/missing PostgreSQL setting: "+key {
				t.Fatalf("missing gate: %v", e)
			}
			good[key] = saved
		})
	}
	bad := good["enable_seqscan"]
	bad.Value = "off"
	good["enable_seqscan"] = bad
	if e := c18CatalogValidateSettings(good); e == nil || e.Error() != "planner override: enable_seqscan" {
		t.Fatalf("planner gate: %v", e)
	}
}

func c18CatalogSyntheticState() c18Catalog {
	c := c18CatalogSynthetic()
	// Explicitly synthetic values; no SQL is generated from this fixture and it
	// cannot enter the candidate producer. The validator sees all required keys.
	for _, rows := range c {
		for _, r := range rows {
			for k, v := range r {
				if v == nil {
					r[k] = "synthetic"
				}
			}
		}
	}
	c["indexes"][0]["live"] = true
	c["constraints"][0]["validated"] = true
	c["dependencies"][0]["reference_schema"] = "pg_catalog"
	c["dependencies"][0]["object_schema"] = "public"
	c["dependencies"][0]["object_resolved"] = true
	c["dependencies"][0]["reference_resolved"] = true
	c["dependencies"][0]["object_in_scope"] = true
	c["dependencies"][0]["reference_in_scope"] = false
	c["dependencies"][0]["object_class"] = "pg_trigger"
	c["dependencies"][0]["reference_class"] = "pg_proc"
	c["comments"][0]["class"] = "pg_trigger"
	c["dependencies"][0]["reference"] = "pg_catalog.gen_random_uuid()"
	externalDefinition := c18CatalogRow{}
	for key, value := range c["functions"][0] {
		externalDefinition[key] = value
	}
	externalDefinition["aggregate"] = nil
	externalDefinition["kind"] = "f"
	c["required_external_objects"] = []c18CatalogRow{{"class": "pg_proc", "identity": "pg_catalog.gen_random_uuid()", "schema": "pg_catalog", "profile": c18CatalogRequiredProfile, "required_by": []any{"signed_bootstrap_profile", "bootstrap_routine:f"}}}
	c["external_objects"] = []c18CatalogRow{{"class": "pg_proc", "identity": c["dependencies"][0]["reference"], "schema": "pg_catalog", "origin_kind": "verified_initdb_bootstrap", "origin_name": "postgresql", "origin_version": "16", "artifact_sha256": strings.Repeat("6", 64), "definition": externalDefinition}}

	toast := c18CatalogRow{}
	for _, q := range c18CatalogQueries {
		if q.Name == "toast" {
			for _, k := range strings.Fields(q.Keys) {
				toast[k] = nil
			}
		}
	}
	toast["table"] = "synthetic"
	toast["kind"] = "t"
	toast["persistence"] = "p"
	toast["owner"] = "synthetic"
	toast["access_method"] = "heap"
	toast["rls"] = false
	toast["forced_rls"] = false
	toast["replica_identity"] = "n"
	cols := []any{}
	for i, name := range []string{"chunk_id", "chunk_seq", "chunk_data"} {
		cols = append(cols, map[string]any{"name": name, "position": json.Number(strconv.Itoa(i + 1)), "type": "synthetic", "typmod": json.Number("-1"), "not_null": true, "storage": "p", "compression": "", "statistics": json.Number("-1"), "options": nil, "acl": nil})
	}
	toast["columns"] = cols
	toast["indexes"] = []any{map[string]any{"definition": "synthetic toast index", "valid": true, "ready": true, "live": true, "unique": true, "options": nil, "tablespace": nil, "access_method": "btree"}}
	c["toast"] = []c18CatalogRow{toast}
	c["db_role_settings"] = []c18CatalogRow{{"database_scope": "owned_database", "role_scope": "all_roles", "position": json.Number("1"), "name": "lock_timeout", "value": "0", "known": true, "context": "user"}}
	c["extensions"] = nil
	for _, name := range []string{"plpgsql", "pgcrypto", "pg_trgm"} {
		r := c18CatalogRow{}
		for _, k := range strings.Fields("name schema version owner relocatable config conditions") {
			r[k] = "synthetic"
		}
		r["name"] = name
		c["extensions"] = append(c["extensions"], r)
	}
	c["goose"] = []c18CatalogRow{}
	for i := 0; i <= 18; i++ {
		c["goose"] = append(c["goose"], c18CatalogRow{"id": json.Number(strconv.Itoa(i + 1)), "version": json.Number(strconv.Itoa(i)), "applied": true})
	}
	return c
}
func TestR5Current18FullCatalogStateFailClosed(t *testing.T) {
	probes := []struct {
		name, want string
		mutate     func(c18Catalog)
	}{
		{"unknown_typmod", "unknown catalog value: columns.typmod", func(c c18Catalog) { c["columns"][0]["typmod"] = nil }},
		{"unknown_migration", "unexpected Goose history", func(c c18Catalog) { c["goose"][18]["version"] = json.Number("19") }},
		{"missing_migration", "unexpected Goose history", func(c c18Catalog) { c["goose"] = c["goose"][:18] }},
		{"unapplied_migration", "unexpected Goose history", func(c c18Catalog) { c["goose"][18]["applied"] = false }},
		{"index_invalid", "unsafe index state", func(c c18Catalog) { c["indexes"][0]["valid"] = false }},
		{"index_not_ready", "unsafe index state", func(c c18Catalog) { c["indexes"][0]["ready"] = false }},
		{"index_not_live", "unsafe index state", func(c c18Catalog) { c["indexes"][0]["live"] = false }},
		{"trigger_disabled", "unsafe trigger state", func(c c18Catalog) { c["triggers"][0]["enabled"] = "D" }},
		{"constraint_not_validated", "unvalidated constraint", func(c c18Catalog) { c["constraints"][0]["validated"] = false }},
		{"outside_dependency", "unsupported dependency owner schema: reference", func(c c18Catalog) { c["dependencies"][0]["reference_schema"] = "outside" }},
		{"unexpected_extension", "unexpected extensions", func(c c18Catalog) { c["extensions"][0]["name"] = "unknown_extension" }},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			c := c18CatalogSyntheticState()
			if e := c18CatalogValidateState(c); e != nil {
				t.Fatal("same-source synthetic state positive:", e)
			}
			p.mutate(c)
			if e := c18CatalogValidateState(c); e == nil || e.Error() != p.want {
				t.Fatalf("state gate: got %v, want %s", e, p.want)
			}
		})
	}
}
func TestR5Current18FullCatalogCanonicalBinding(t *testing.T) {
	type unsorted struct {
		Z json.Number `json:"z"`
		A string      `json:"a"`
	}
	raw, e := c18CatalogBytes(unsorted{json.Number("9223372036854775807"), "<中文>"})
	if e != nil {
		t.Fatal(e)
	}
	if string(raw) != `{"a":"<中文>","z":9223372036854775807}` {
		t.Fatalf("canonical numeric/order mismatch: %s", raw)
	}
	reordered := []byte(`{"z":9223372036854775807,"a":"<中文>"}`)
	if c18CatalogSHA(raw) == c18CatalogSHA(reordered) {
		t.Fatal("raw binding pin accepted reordered bytes")
	}
}

func TestR5Current18FullCatalogScopeFailClosed(t *testing.T) {
	good := map[string]json.Number{}
	for _, k := range strings.Fields(c18CatalogScopeKeys) {
		good[k] = "0"
	}
	if e := c18CatalogValidateScope(good); e != nil {
		t.Fatal(e)
	}
	for _, key := range strings.Fields(c18CatalogScopeKeys) {
		t.Run(key, func(t *testing.T) {
			if e := c18CatalogValidateScope(good); e != nil {
				t.Fatal("same-source scope positive:", e)
			}
			good[key] = "1"
			if e := c18CatalogValidateScope(good); e == nil || e.Error() != "unsupported catalog scope: "+key {
				t.Fatalf("scope gate: %v", e)
			}
			delete(good, key)
			good["unexpected"] = "0"
			if e := c18CatalogValidateScope(good); e == nil || e.Error() != "scope coverage changed" {
				t.Fatalf("unknown/missing scope gate: %v", e)
			}
			delete(good, "unexpected")
			good[key] = "0"
		})
	}
}

// These are real SQL catalog mutations/reads in the future owned target, not
// claims that the seven pure V1 tests validated any SQL. None are executed by
// reading/compiling this source. The role case additionally needs root's explicit
// transaction-owned shared-role scope; source flags are not an owner receipt.
func c18CatalogOwnedSQLCoverage(t *testing.T, ctx context.Context, o *c18CatalogOwned, b c18CatalogBinding, pin string, reference c18Catalog, ledger *c18CatalogLedger) {
	t.Helper()
	if os.Getenv("TABMAIL_C18_CATALOG_ROLE_PROBES") != "transaction_owned_roles" {
		t.Fatal("SQL coverage needs separately approved transaction-owned role probes")
	}
	txCase := func(name string, body func(*testing.T, pgx.Tx)) {
		c18CatalogRunCase(t, ledger, name, func(t *testing.T) {
			tx, e := o.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
			if e != nil {
				t.Fatal(e)
			}
			defer func() {
				rollback, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if e := tx.Rollback(rollback); e != nil && e != pgx.ErrTxClosed {
					t.Errorf("SQL coverage rollback: %T", e)
				}
			}()
			if _, e = tx.Exec(ctx, `SET LOCAL search_path = pg_catalog, public`); e != nil {
				t.Fatal(e)
			}
			positive, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
			if e != nil {
				t.Fatal(e)
			}
			if e = c18CatalogCompare(reference, positive.Catalog); e != nil {
				t.Fatal("SQL coverage baseline:", e)
			}
			body(t, tx)
		})
	}
	compareGate := func(t *testing.T, tx pgx.Tx, baseline c18Catalog, want string) c18Catalog {
		t.Helper()
		changed, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
		if e == nil {
			e = c18CatalogCompare(baseline, changed.Catalog)
		}
		if e == nil || e.Error() != want {
			t.Fatalf("SQL coverage gate: got %v, want %s", e, want)
		}
		return changed.Catalog
	}
	txCase("sql_trigger_extension_dependency", func(t *testing.T, tx pgx.Tx) {
		if _, e := tx.Exec(ctx, `ALTER TRIGGER permission_profile_revision ON public.permission_profiles DEPENDS ON EXTENSION pg_trgm`); e != nil {
			t.Fatal(e)
		}
		changed := compareGate(t, tx, reference, "catalog drift: dependencies")
		found := false
		for _, r := range changed["dependencies"] {
			if r["object_class"] == "pg_trigger" && r["reference_class"] == "pg_extension" && r["reference"] == "pg_trgm" && r["kind"] == "x" {
				found = true
			}
		}
		if !found {
			t.Fatal("trigger attachment extension edge not observed")
		}
	})
	txCase("sql_trigger_comment", func(t *testing.T, tx pgx.Tx) {
		if _, e := tx.Exec(ctx, `COMMENT ON TRIGGER permission_profile_revision ON public.permission_profiles IS 'c18-trigger-comment-probe'`); e != nil {
			t.Fatal(e)
		}
		compareGate(t, tx, reference, "catalog drift: comments")
	})
	for _, probe := range []struct{ name, setup, mutation, class string }{
		{"sql_rule_comment", `CREATE VIEW public.c18_catalog_view AS SELECT id FROM public.tenants`, `COMMENT ON RULE "_RETURN" ON public.c18_catalog_view IS 'c18-rule-comment-probe'`, "pg_rewrite"},
		{"sql_policy_comment", `CREATE POLICY c18_catalog_policy ON public.tenants USING (true)`, `COMMENT ON POLICY c18_catalog_policy ON public.tenants IS 'c18-policy-comment-probe'`, "pg_policy"},
	} {
		txCase(probe.name, func(t *testing.T, tx pgx.Tx) {
			if _, e := tx.Exec(ctx, probe.setup); e != nil {
				t.Fatal(e)
			}
			baseline, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
			if e != nil {
				t.Fatal("attachment SQL positive:", e)
			}
			seen := false
			for _, r := range baseline.Catalog["dependencies"] {
				if r["object_class"] == probe.class || r["reference_class"] == probe.class {
					seen = true
				}
			}
			if !seen {
				t.Fatal("attachment dependency absent from positive SQL collection")
			}
			if _, e = tx.Exec(ctx, probe.mutation); e != nil {
				t.Fatal(e)
			}
			compareGate(t, tx, baseline.Catalog, "catalog drift: comments")
		})
	}
	txCase("sql_attached_dependency_address_coverage", func(t *testing.T, tx pgx.Tx) {
		// Independent ownership joins produce the expected exact addresses. Comparing
		// them to the production scope catches a NULL-schema omission, even when the
		// final serialized rows happen to have unchanged definitions.
		expected := `WITH expected(classid,objid) AS (
   SELECT 'pg_trigger'::regclass,t.oid FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace
   UNION SELECT 'pg_attrdef'::regclass,d.oid FROM pg_attrdef d JOIN pg_class c ON c.oid=d.adrelid WHERE c.relnamespace='public'::regnamespace
   UNION SELECT 'pg_rewrite'::regclass,r.oid FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class WHERE c.relnamespace='public'::regnamespace
   UNION SELECT 'pg_policy'::regclass,p.oid FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid WHERE c.relnamespace='public'::regnamespace
   UNION SELECT 'pg_amop'::regclass,a.oid FROM pg_amop a JOIN pg_opfamily f ON f.oid=a.amopfamily WHERE f.opfnamespace='public'::regnamespace
   UNION SELECT 'pg_amproc'::regclass,a.oid FROM pg_amproc a JOIN pg_opfamily f ON f.oid=a.amprocfamily WHERE f.opfnamespace='public'::regnamespace
  ) SELECT classid::regclass::text,objid::bigint,EXISTS(SELECT 1 FROM object_scope s WHERE s.classid=expected.classid AND s.objid=expected.objid) FROM expected ORDER BY 1,2`
		rows, e := tx.Query(ctx, c18CatalogObjectScopeCTE+", "+strings.TrimPrefix(expected, "WITH "))
		if e != nil {
			t.Fatal(e)
		}
		type address struct {
			class   string
			oid     int64
			inScope bool
		}
		addresses := []address{}
		for rows.Next() {
			var a address
			if e = rows.Scan(&a.class, &a.oid, &a.inScope); e != nil {
				rows.Close()
				t.Fatal(e)
			}
			addresses = append(addresses, a)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			t.Fatal(e)
		}
		witnessed := map[string]bool{}
		for _, a := range addresses {
			if !a.inScope {
				t.Fatalf("attached address missing: class=%s", a.class)
			}
			witnessed[a.class] = true
		}
		for _, class := range []string{"pg_trigger", "pg_attrdef", "pg_amop", "pg_amproc"} {
			if !witnessed[class] {
				t.Fatalf("missing actual SQL coverage witness: %s", class)
			}
		}
	})
	txCase("sql_custom_pg_catalog_function", func(t *testing.T, tx pgx.Tx) {
		// This scope is separately signed by owner payload v2. The existing
		// information_schema permission is not reused for pg_catalog DDL.
		name := o.name + "_external"
		identifier := pgx.Identifier{"pg_catalog", name}.Sanitize()
		if _, e := tx.Exec(ctx, "CREATE FUNCTION "+identifier+"() RETURNS uuid LANGUAGE SQL BEGIN ATOMIC SELECT id FROM public.tenants WHERE true LIMIT 1; END"); e != nil {
			t.Fatal(e)
		}
		inspect := func() (string, string) {
			var def, edges string
			e := tx.QueryRow(ctx, `SELECT pg_get_functiondef(p.oid),(SELECT COALESCE(jsonb_agg(jsonb_build_array(d.objsubid,d.refclassid::regclass::text,d.refobjid,d.refobjsubid,d.deptype) ORDER BY d.objsubid,d.refclassid,d.refobjid,d.refobjsubid,d.deptype),'[]'::jsonb)::text FROM pg_depend d WHERE d.classid='pg_proc'::regclass AND d.objid=p.oid) FROM pg_proc p WHERE p.pronamespace='pg_catalog'::regnamespace AND p.proname=$1 AND p.pronargs=0`, name).Scan(&def, &edges)
			if e != nil {
				t.Fatal(e)
			}
			return def, edges
		}
		beforeDef, beforeEdges := inspect()
		compareGate(t, tx, reference, "unapproved external dependency object")
		if _, e := tx.Exec(ctx, "CREATE OR REPLACE FUNCTION "+identifier+"() RETURNS uuid LANGUAGE SQL BEGIN ATOMIC SELECT id FROM public.tenants WHERE false LIMIT 1; END"); e != nil {
			t.Fatal(e)
		}
		afterDef, afterEdges := inspect()
		if beforeDef == afterDef || beforeEdges != afterEdges {
			t.Fatal("pg_catalog counterexample requires definition change with unchanged dependency edges")
		}
		compareGate(t, tx, reference, "unapproved external dependency object")
	})
	txCase("sql_external_attached_view", func(t *testing.T, tx pgx.Tx) {
		// Dedicated new fixture DB only, signed scope above. CREATE (not replace)
		// cannot overwrite an existing system object; the entire case rolls back.
		if _, e := tx.Exec(ctx, `CREATE VIEW information_schema.c18_view AS SELECT id FROM public.tenants WHERE true`); e != nil {
			t.Fatal(e)
		}
		edges := func() string {
			var raw string
			e := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(d.classid::regclass::text,d.objsubid,d.refclassid::regclass::text,d.refobjid,d.refobjsubid,d.deptype) ORDER BY d.classid,d.objsubid,d.refclassid,d.refobjid,d.refobjsubid,d.deptype),'[]'::jsonb)::text FROM pg_depend d WHERE d.classid='pg_rewrite'::regclass AND d.objid=(SELECT oid FROM pg_rewrite WHERE ev_class='information_schema.c18_view'::regclass AND rulename='_RETURN')`).Scan(&raw)
			if e != nil {
				t.Fatal(e)
			}
			return raw
		}
		beforeEdges := edges()
		var beforeDef, afterDef string
		if e := tx.QueryRow(ctx, `SELECT pg_get_viewdef('information_schema.c18_view'::regclass,false)`).Scan(&beforeDef); e != nil {
			t.Fatal(e)
		}
		compareGate(t, tx, reference, "unsupported dependency owner schema: object")
		if _, e := tx.Exec(ctx, `CREATE OR REPLACE VIEW information_schema.c18_view AS SELECT id FROM public.tenants WHERE false`); e != nil {
			t.Fatal(e)
		}
		if e := tx.QueryRow(ctx, `SELECT pg_get_viewdef('information_schema.c18_view'::regclass,false)`).Scan(&afterDef); e != nil {
			t.Fatal(e)
		}
		if beforeDef == afterDef || beforeEdges != edges() {
			t.Fatal("external definition counterexample must change definition without changing dependency edges")
		}
		compareGate(t, tx, reference, "unsupported dependency owner schema: object")
	})
	txCase("sql_toast_options", func(t *testing.T, tx pgx.Tx) {
		var exists bool
		if e := tx.QueryRow(ctx, `SELECT reltoastrelid<>0 FROM pg_class WHERE oid='public.tenants'::regclass`).Scan(&exists); e != nil || !exists {
			t.Fatal("TOAST positive relation absent")
		}
		if _, e := tx.Exec(ctx, `ALTER TABLE public.tenants SET (toast.autovacuum_enabled = false)`); e != nil {
			t.Fatal(e)
		}
		compareGate(t, tx, reference, "unsupported TOAST options")
	})
	txCase("sql_acl_member_direction", func(t *testing.T, tx pgx.Tx) {
		var super bool
		if e := tx.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&super); e != nil || !super {
			t.Fatal("owned role SQL probe requires separately attested fixture superuser")
		}
		a, member := o.name+"_acl", o.name+"_member"
		for _, name := range []string{a, member} {
			if _, e := tx.Exec(ctx, "CREATE ROLE "+pgx.Identifier{name}.Sanitize()+" NOLOGIN"); e != nil {
				t.Fatalf("CREATE owned role: %T", e)
			}
		}
		if _, e := tx.Exec(ctx, "GRANT SELECT ON public.tenants TO "+pgx.Identifier{a}.Sanitize()); e != nil {
			t.Fatal(e)
		}
		before, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
		if e != nil {
			t.Fatal("ACL SQL positive:", e)
		}
		for _, r := range before.Catalog["roles"] {
			if r["name"] == member {
				t.Fatal("membership direction positive must not already reach isolated member")
			}
		}
		if _, e = tx.Exec(ctx, "GRANT "+pgx.Identifier{a}.Sanitize()+" TO "+pgx.Identifier{member}.Sanitize()); e != nil {
			t.Fatal(e)
		}
		after := compareGate(t, tx, before.Catalog, "catalog drift: roles")
		roleSeen, edgeSeen := false, false
		for _, r := range after["roles"] {
			roleSeen = roleSeen || r["name"] == member
		}
		for _, r := range after["role_memberships"] {
			edgeSeen = edgeSeen || (r["role"] == a && r["member"] == member)
		}
		if !roleSeen || !edgeSeen {
			t.Fatal("ACL downward member/edge omitted")
		}
	})
	if !t.Failed() {
		var residual int
		if e := o.store.pool.QueryRow(ctx, `SELECT count(*) FROM pg_roles WHERE rolname=ANY($1::text[])`, []string{o.name + "_acl", o.name + "_member"}).Scan(&residual); e != nil || residual != 0 {
			t.Fatal("table ACL case role rollback residual/unknown")
		}
	}
	txCase("sql_database_acl_member_direction", func(t *testing.T, tx pgx.Tx) {
		var super bool
		if e := tx.QueryRow(ctx, `SELECT rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&super); e != nil || !super {
			t.Fatal("owned role SQL probe requires separately attested fixture superuser")
		}
		a, member := o.name+"_acl", o.name+"_member"
		for _, name := range []string{a, member} {
			if _, e := tx.Exec(ctx, "CREATE ROLE "+pgx.Identifier{name}.Sanitize()+" NOLOGIN"); e != nil {
				t.Fatalf("CREATE owned role: %T", e)
			}
		}
		if _, e := tx.Exec(ctx, "GRANT CREATE ON DATABASE "+pgx.Identifier{o.name}.Sanitize()+" TO "+pgx.Identifier{a}.Sanitize()); e != nil {
			t.Fatal(e)
		}
		var localDependencies int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM pg_shdepend WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND refclassid='pg_authid'::regclass AND refobjid=(SELECT oid FROM pg_roles WHERE rolname=$1)`, a).Scan(&localDependencies); e != nil || localDependencies != 0 {
			t.Fatal("DB-only ACL positive must have no database-local role dependency")
		}
		before, e := c18CatalogCollect(ctx, tx, b, pin, o.name, "owned_transaction_negative_control")
		if e != nil {
			t.Fatal("ACL SQL positive:", e)
		}
		aSeen := false
		for _, r := range before.Catalog["roles"] {
			aSeen = aSeen || r["name"] == a
		}
		if !aSeen {
			t.Fatal("DB-only grantee missing before membership mutation")
		}
		for _, r := range before.Catalog["roles"] {
			if r["name"] == member {
				t.Fatal("membership direction positive must not already reach isolated member")
			}
		}
		if _, e = tx.Exec(ctx, "GRANT "+pgx.Identifier{a}.Sanitize()+" TO "+pgx.Identifier{member}.Sanitize()); e != nil {
			t.Fatal(e)
		}
		after := compareGate(t, tx, before.Catalog, "catalog drift: roles")
		roleSeen, edgeSeen := false, false
		for _, r := range after["roles"] {
			roleSeen = roleSeen || r["name"] == member
		}
		for _, r := range after["role_memberships"] {
			edgeSeen = edgeSeen || (r["role"] == a && r["member"] == member)
		}
		if !roleSeen || !edgeSeen {
			t.Fatal("ACL downward member/edge omitted")
		}
	})
	if t.Failed() {
		t.Fatal("owned SQL coverage failed; candidate not written")
	}
	var residualFunctions int
	if e := o.store.pool.QueryRow(ctx, `SELECT count(*) FROM pg_proc WHERE pronamespace='pg_catalog'::regnamespace AND proname=$1`, o.name+"_external").Scan(&residualFunctions); e != nil || residualFunctions != 0 {
		t.Fatal("owned pg_catalog function rollback residual/unknown")
	}

	var residualRoles int
	if e := o.store.pool.QueryRow(ctx, `SELECT count(*) FROM pg_roles WHERE rolname=ANY($1::text[])`, []string{o.name + "_acl", o.name + "_member"}).Scan(&residualRoles); e != nil || residualRoles != 0 {
		t.Fatalf("owned role rollback residual/unknown: %s_acl %s_member err=%T", o.name, o.name, e)
	}
	c18CatalogRunCase(t, ledger, "sql_persistent_lock_timeout_store_reopen", func(t *testing.T) {
		// This case commits ONLY a setting on the owned database, then resets it. It
		// does not alter role-global settings or restart the native PostgreSQL server.
		positive, e := c18CatalogSnapshot(ctx, o.store, b, pin, o.name, "official_new_pre_population")
		if e != nil {
			t.Fatal(e)
		}
		if e = c18CatalogCompare(reference, positive.Catalog); e != nil {
			t.Fatal("persistent SQL positive:", e)
		}
		reset := func() error {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, e := o.admin.Exec(cleanup, "ALTER DATABASE "+pgx.Identifier{o.name}.Sanitize()+" RESET lock_timeout")
			return e
		}
		defer func() {
			if e := reset(); e != nil {
				t.Errorf("owned persistent setting reset failed: %T", e)
			}
		}()
		if _, e = o.admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{o.name}.Sanitize()+" SET lock_timeout = '11s'"); e != nil {
			t.Fatal(e)
		}
		observed, e := c18CatalogSnapshot(ctx, o.store, b, pin, o.name, "official_new_pre_population")
		if e != nil {
			t.Fatal(e)
		}
		if e = c18CatalogCompare(reference, observed.Catalog); e == nil || e.Error() != "catalog drift: db_role_settings" {
			t.Fatalf("persistent-before-reconnect gate: %v", e)
		}
		_ = o.store.Close()
		o.store = nil
		o.store, e = New(ctx, o.cfg)
		if e != nil {
			t.Fatalf("setting probe store reopen: %T", e)
		}
		observed, e = c18CatalogSnapshot(ctx, o.store, b, pin, o.name, "official_store_reopen_pre_population")
		if e != nil {
			t.Fatal(e)
		}
		if e = c18CatalogCompare(reference, observed.Catalog); e == nil || e.Error() != "catalog drift: db_role_settings" {
			t.Fatalf("persistent-after-reconnect gate: %v", e)
		}
		if e = reset(); e != nil {
			t.Fatal("owned setting reset failed")
		}
		_ = o.store.Close()
		o.store = nil
		o.store, e = New(ctx, o.cfg)
		if e != nil {
			t.Fatalf("reset store reopen: %T", e)
		}
	})
	if t.Failed() {
		t.Fatal("owned persistent SQL coverage failed; candidate not written")
	}
	restored, e := c18CatalogSnapshot(ctx, o.store, b, pin, o.name, "official_new_pre_population")
	if e != nil {
		t.Fatal(e)
	}
	if e = c18CatalogCompare(reference, restored.Catalog); e != nil {
		t.Fatal("SQL coverage did not restore original catalog:", e)
	}
}

type c18CatalogFakeRow struct {
	exists bool
	err    error
}

func (r c18CatalogFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return fmt.Errorf("fake row arity")
	}
	p, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("fake row type")
	}
	*p = r.exists
	return nil
}

type c18CatalogFakeAdmin struct {
	dropErr, queryErr, closeErr error
	exists                      bool
	closeCalls                  int
	closeContextCanceled        bool
}

func (a *c18CatalogFakeAdmin) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, a.dropErr
}
func (a *c18CatalogFakeAdmin) QueryRow(context.Context, string, ...any) pgx.Row {
	return c18CatalogFakeRow{a.exists, a.queryErr}
}
func (a *c18CatalogFakeAdmin) Close(ctx context.Context) error {
	a.closeCalls++
	a.closeContextCanceled = ctx.Err() != nil
	return a.closeErr
}

func TestR5Current18FullCatalogCleanupAlwaysClosesAdmin(t *testing.T) {
	for _, p := range []struct {
		name                        string
		dropErr, queryErr, closeErr error
		exists, residual            bool
	}{
		{name: "success"},
		{name: "drop_failed", dropErr: errors.New("synthetic drop failure"), residual: true},
		{name: "verification_failed", queryErr: errors.New("synthetic verification failure"), residual: true},
		{name: "database_remains", exists: true, residual: true},
		{name: "close_failed", closeErr: errors.New("synthetic close failure")},
	} {
		t.Run(p.name, func(t *testing.T) {
			good := &c18CatalogFakeAdmin{}
			owner := &c18CatalogOwned{admin: good, name: "tm_c18_catalog_synthetic", created: true}
			if e := owner.close(context.Background()); e != nil || good.closeCalls != 1 || owner.created {
				t.Fatal("cleanup same-source positive")
			}
			admin := &c18CatalogFakeAdmin{dropErr: p.dropErr, queryErr: p.queryErr, closeErr: p.closeErr, exists: p.exists}
			owner = &c18CatalogOwned{admin: admin, name: "tm_c18_catalog_synthetic", created: true}
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			e := owner.close(canceled)
			if admin.closeCalls != 1 || admin.closeContextCanceled || owner.admin != nil || owner.created != p.residual {
				t.Fatal("admin close/residual state mismatch")
			}
			if p.residual && (e == nil || !strings.Contains(e.Error(), "owned database residual or cleanup unknown: tm_c18_catalog_synthetic")) {
				t.Fatalf("residual gate: %v", e)
			}
			if p.closeErr != nil && (e == nil || !strings.Contains(e.Error(), "admin close failed:")) {
				t.Fatalf("close failure gate: %v", e)
			}
			if p.name == "success" && e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestR5Current18FullCatalogSupplementalFailClosed(t *testing.T) {
	probes := []struct {
		name, want string
		mutate     func(c18Catalog)
	}{
		{"unknown_dependency_class", "unsupported dependency class: object_class", func(c c18Catalog) { c["dependencies"][0]["object_class"] = "pg_unknown" }},
		{"unknown_reference_class", "unsupported dependency class: reference_class", func(c c18Catalog) { c["dependencies"][0]["reference_class"] = "pg_unknown" }},
		{"unresolved_attached_owner", "unresolved dependency owner: object", func(c c18Catalog) { c["dependencies"][0]["object_resolved"] = false }},
		{"null_attached_owner", "unresolved dependency owner: object", func(c c18Catalog) { c["dependencies"][0]["object_schema"] = nil }},
		{"external_information_schema_owner", "unsupported dependency owner schema: object", func(c c18Catalog) { c["dependencies"][0]["object_schema"] = "information_schema" }},
		{"external_information_schema_reference", "unsupported dependency owner schema: reference", func(c c18Catalog) { c["dependencies"][0]["reference_schema"] = "information_schema" }},
		{"toast_nondefault_options", "unsupported TOAST options", func(c c18Catalog) { c["toast"][0]["options"] = []any{"autovacuum_enabled=false"} }},
		{"unknown_persistent_setting", "unknown persistent setting", func(c c18Catalog) {
			c["db_role_settings"][0]["known"] = false
		}},
		{"unsupported_persistent_setting", "unsupported persistent setting: enable_seqscan", func(c c18Catalog) {
			c["db_role_settings"][0]["name"] = "enable_seqscan"
		}},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			c := c18CatalogSyntheticState()
			if e := c18CatalogValidateState(c); e != nil {
				t.Fatal("same-source synthetic positive:", e)
			}
			p.mutate(c)
			if e := c18CatalogValidateState(c); e == nil || e.Error() != p.want {
				t.Fatalf("supplemental gate: got %v, want %s", e, p.want)
			}
		})
	}
}

// V2 had fourteen children; V3 added DB-only ACL and information_schema cases;
// V4 adds the separately authorized pg_catalog custom-function case. A -run filter may skip any t.Run body: the
// parent's success bit is never a substitute for this exact seventeen-case set.
const c18CatalogRequiredCases = "varchar255_to_1 trigger_disabled sequence_cache sequence_ownership function_definition outside_schema sql_trigger_extension_dependency sql_trigger_comment sql_rule_comment sql_policy_comment sql_attached_dependency_address_coverage sql_toast_options sql_acl_member_direction sql_database_acl_member_direction sql_external_attached_view sql_persistent_lock_timeout_store_reopen sql_custom_pg_catalog_function"

type c18CatalogLedger struct {
	required  map[string]bool
	completed map[string]bool
}

func c18CatalogNewLedger() *c18CatalogLedger {
	l := &c18CatalogLedger{map[string]bool{}, map[string]bool{}}
	for _, name := range strings.Fields(c18CatalogRequiredCases) {
		l.required[name] = true
	}
	return l
}
func (l *c18CatalogLedger) requiredList() []string {
	a := []string{}
	for name := range l.required {
		a = append(a, name)
	}
	sort.Strings(a)
	return a
}
func (l *c18CatalogLedger) completedList() []string {
	a := []string{}
	for name, passed := range l.completed {
		if passed {
			a = append(a, name)
		}
	}
	sort.Strings(a)
	return a
}
func (l *c18CatalogLedger) record(name string, started, returned, passed, runOK bool) error {
	if !l.required[name] {
		return fmt.Errorf("unknown owned case: %s", name)
	}
	if started && returned && passed && runOK {
		if l.completed[name] {
			return fmt.Errorf("duplicate completed owned case: %s", name)
		}
		l.completed[name] = true
	}
	return nil
}
func (l *c18CatalogLedger) validate() error {
	if len(l.required) != len(strings.Fields(c18CatalogRequiredCases)) {
		return fmt.Errorf("invalid required owned case set")
	}
	for name := range l.completed {
		if !l.required[name] {
			return fmt.Errorf("unknown completed owned case: %s", name)
		}
	}
	for _, name := range l.requiredList() {
		if !l.completed[name] {
			return fmt.Errorf("owned case incomplete: %s", name)
		}
	}
	return nil
}
func c18CatalogRunCase(t *testing.T, l *c18CatalogLedger, name string, body func(*testing.T)) {
	t.Helper()
	started, returned, passed := false, false, false
	runOK := t.Run(name, func(child *testing.T) {
		started = true
		body(child)
		returned = true
		passed = !child.Failed() && !child.Skipped()
	})
	// t.Run returns only after defers/Cleanup. A rollback/cleanup failure therefore
	// prevents registration even if the body reached its last statement.
	if e := l.record(name, started, returned, passed, runOK); e != nil {
		t.Fatal(e)
	}
}
func TestR5Current18FullCatalogRequiredCaseLedger(t *testing.T) {
	full := func() *c18CatalogLedger {
		l := c18CatalogNewLedger()
		for _, name := range l.requiredList() {
			if e := l.record(name, true, true, true, true); e != nil {
				t.Fatal(e)
			}
		}
		if e := l.validate(); e != nil {
			t.Fatal("complete same-source positive:", e)
		}
		return l
	}
	for _, name := range c18CatalogNewLedger().requiredList() {
		t.Run(name, func(t *testing.T) {
			l := full()
			delete(l.completed, name)
			if e := l.validate(); e == nil || e.Error() != "owned case incomplete: "+name {
				t.Fatalf("incomplete gate: %v", e)
			}
		})
	}
	for _, p := range []struct {
		name                             string
		started, returned, passed, runOK bool
	}{
		{"filtered", false, false, false, true}, {"skipped", true, false, false, true}, {"fatal", true, false, false, false}, {"failed", true, true, false, false}, {"cleanup_failed", true, true, true, false},
	} {
		t.Run(p.name, func(t *testing.T) {
			l := full()
			name := l.requiredList()[0]
			delete(l.completed, name)
			if e := l.record(name, p.started, p.returned, p.passed, p.runOK); e != nil {
				t.Fatal(e)
			}
			if e := l.validate(); e == nil || e.Error() != "owned case incomplete: "+name {
				t.Fatalf("execution registration gate: %v", e)
			}
		})
	}
}

// This context is an external root trust input pinned by Binding.ContextSHA256,
// not an environment flag and not a receipt this collector issues. No runtime
// producer for it is supplied here. The owner receipt must be signed by its key,
// tied to this source/attempt, and matched to the actual PG cluster before CREATE.
type c18CatalogRootContext struct {
	Kind                 string `json:"kind"`
	AttemptID            string `json:"attempt_id"`
	SourceSHA            string `json:"source_sha"`
	SourceManifestSHA256 string `json:"source_manifest_sha256"`
	OwnerPublicKeyHex    string `json:"owner_public_key_hex"`
	OwnerReceiptSHA256   string `json:"owner_receipt_sha256"`
	C1809ReceiptSHA256   string `json:"c18_09_receipt_sha256"`
}
type c18CatalogOwnerPayload struct {
	Kind                           string   `json:"kind"`
	AttemptID                      string   `json:"attempt_id"`
	SourceSHA                      string   `json:"source_sha"`
	SourceManifestSHA256           string   `json:"source_manifest_sha256"`
	C1809ReceiptSHA256             string   `json:"c18_09_receipt_sha256"`
	Host                           string   `json:"host"`
	Port                           uint16   `json:"port"`
	AdminDatabase                  string   `json:"admin_database"`
	Role                           string   `json:"role"`
	SystemIdentifier               string   `json:"system_identifier"`
	PostmasterStartUnixMicro       int64    `json:"postmaster_start_unix_micro"`
	IssuedUnixSeconds              int64    `json:"issued_unix_seconds"`
	ExpiresUnixSeconds             int64    `json:"expires_unix_seconds"`
	DedicatedNewFixtureCluster     bool     `json:"dedicated_new_fixture_cluster"`
	FixtureSuperuser               bool     `json:"fixture_superuser"`
	TransactionOnlyNOLOGINRoles    int      `json:"transaction_only_nologin_roles"`
	RoleCases                      []string `json:"role_cases"`
	PGCatalogFunctionRollbackProbe bool     `json:"pg_catalog_function_rollback_probe"`
	ExternalReferenceSHA256        string   `json:"external_reference_sha256"`
	InformationSchemaRollbackProbe bool     `json:"information_schema_rollback_probe"`
	MaxOwnedDatabases              int      `json:"max_owned_databases"`
	NativeRestart                  bool     `json:"native_restart"`
	Benchmark                      bool     `json:"benchmark"`
}
type c18CatalogOwnerReceipt struct {
	Payload      c18CatalogOwnerPayload `json:"payload"`
	SignatureHex string                 `json:"signature_hex"`
}

func c18CatalogValidateOwner(contextRaw, receiptRaw []byte, b c18CatalogBinding, now time.Time) (c18CatalogOwnerReceipt, error) {
	var root c18CatalogRootContext
	var receipt c18CatalogOwnerReceipt
	if len(contextRaw) == 0 || c18CatalogSHA(contextRaw) != b.ContextSHA256 {
		return receipt, fmt.Errorf("missing/mismatched external root context")
	}
	if e := c18CatalogDecode(contextRaw, &root); e != nil {
		return receipt, fmt.Errorf("external root context grammar")
	}
	canonical, e := c18CatalogBytes(root)
	if e != nil || !bytes.Equal(canonical, contextRaw) {
		return receipt, fmt.Errorf("noncanonical external root context")
	}
	if root.Kind != "c18_catalog_external_root_context_v1" || root.AttemptID != b.AttemptID || root.SourceSHA != b.SourceSHA || root.SourceManifestSHA256 != b.SourceManifestSHA256 || !c18CatalogHashValid(root.C1809ReceiptSHA256, 32) {
		return receipt, fmt.Errorf("external root source/C18-09 binding")
	}
	if !c18CatalogHashValid(root.OwnerReceiptSHA256, 32) || len(receiptRaw) == 0 || c18CatalogSHA(receiptRaw) != root.OwnerReceiptSHA256 {
		return receipt, fmt.Errorf("missing/mismatched fixture owner receipt")
	}
	if e = c18CatalogDecode(receiptRaw, &receipt); e != nil {
		return receipt, fmt.Errorf("fixture owner receipt grammar")
	}
	canonical, e = c18CatalogBytes(receipt)
	if e != nil || !bytes.Equal(canonical, receiptRaw) {
		return receipt, fmt.Errorf("noncanonical fixture owner receipt")
	}
	public, e := hex.DecodeString(root.OwnerPublicKeyHex)
	if e != nil || len(public) != ed25519.PublicKeySize {
		return receipt, fmt.Errorf("external root owner key")
	}
	signature, e := hex.DecodeString(receipt.SignatureHex)
	if e != nil || len(signature) != ed25519.SignatureSize {
		return receipt, fmt.Errorf("fixture owner signature")
	}
	payload, e := c18CatalogBytes(receipt.Payload)
	if e != nil || !ed25519.Verify(ed25519.PublicKey(public), payload, signature) {
		return receipt, fmt.Errorf("fixture owner signature")
	}
	p := receipt.Payload
	if p.Kind != "c18_catalog_dedicated_fixture_owner_v2" || p.AttemptID != b.AttemptID || p.SourceSHA != b.SourceSHA || p.SourceManifestSHA256 != b.SourceManifestSHA256 || p.C1809ReceiptSHA256 != root.C1809ReceiptSHA256 {
		return receipt, fmt.Errorf("fixture owner source/C18-09 binding")
	}
	if !p.DedicatedNewFixtureCluster || !p.FixtureSuperuser || p.TransactionOnlyNOLOGINRoles != 2 || !reflect.DeepEqual(p.RoleCases, []string{"owned_table_select_then_membership", "owned_database_create_then_membership"}) || !p.InformationSchemaRollbackProbe || !p.PGCatalogFunctionRollbackProbe || !c18CatalogHashValid(p.ExternalReferenceSHA256, 32) || p.MaxOwnedDatabases != 2 || p.NativeRestart || p.Benchmark {
		return receipt, fmt.Errorf("fixture owner scope")
	}
	if p.Host == "" || p.Port == 0 || p.AdminDatabase == "" || p.Role == "" || p.SystemIdentifier == "" || p.PostmasterStartUnixMicro <= 0 {
		return receipt, fmt.Errorf("fixture owner cluster identity")
	}
	if e = c18CatalogOwnerTTL(p.IssuedUnixSeconds, p.ExpiresUnixSeconds, now.Unix()); e != nil {
		return receipt, e
	}
	return receipt, nil
}
func c18CatalogLoadOwner(t *testing.T, b c18CatalogBinding) (c18CatalogOwnerReceipt, string) {
	t.Helper()
	contextRaw, e := os.ReadFile(os.Getenv("TABMAIL_C18_CATALOG_ROOT_CONTEXT"))
	if e != nil {
		t.Fatal("external root context required before fixture connection")
	}
	receiptRaw, e := os.ReadFile(os.Getenv("TABMAIL_C18_CATALOG_OWNER_RECEIPT"))
	if e != nil {
		t.Fatal("signed fixture owner receipt required before fixture connection")
	}
	receipt, e := c18CatalogValidateOwner(contextRaw, receiptRaw, b, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	return receipt, c18CatalogSHA(receiptRaw)
}

func TestR5Current18FullCatalogOwnerReceiptFailClosed(t *testing.T) {
	// Pure signature grammar only. This deterministic test key is never loaded by
	// the runtime producer and does not constitute an external root trust pin.
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{11}, ed25519.SeedSize))
	b := c18CatalogBinding{AttemptID: "synthetic-owner-grammar", SourceSHA: strings.Repeat("1", 40), SourceManifestSHA256: strings.Repeat("2", 64)}
	now := time.Unix(1700000200, 0)
	base := c18CatalogOwnerPayload{Kind: "c18_catalog_dedicated_fixture_owner_v2", AttemptID: b.AttemptID, SourceSHA: b.SourceSHA, SourceManifestSHA256: b.SourceManifestSHA256, C1809ReceiptSHA256: strings.Repeat("3", 64), Host: "synthetic.invalid", Port: 5432, AdminDatabase: "synthetic_admin", Role: "synthetic_owner", SystemIdentifier: "synthetic-system-id", PostmasterStartUnixMicro: 1000000, IssuedUnixSeconds: 1700000000, ExpiresUnixSeconds: 1700000300, DedicatedNewFixtureCluster: true, FixtureSuperuser: true, TransactionOnlyNOLOGINRoles: 2, RoleCases: []string{"owned_table_select_then_membership", "owned_database_create_then_membership"}, InformationSchemaRollbackProbe: true, PGCatalogFunctionRollbackProbe: true, ExternalReferenceSHA256: strings.Repeat("4", 64), MaxOwnedDatabases: 2}
	encode := func(p c18CatalogOwnerPayload) ([]byte, []byte, c18CatalogBinding) {
		payload, e := c18CatalogBytes(p)
		if e != nil {
			t.Fatal(e)
		}
		receipt := c18CatalogOwnerReceipt{p, hex.EncodeToString(ed25519.Sign(key, payload))}
		raw, e := c18CatalogBytes(receipt)
		if e != nil {
			t.Fatal(e)
		}
		root := c18CatalogRootContext{Kind: "c18_catalog_external_root_context_v1", AttemptID: b.AttemptID, SourceSHA: b.SourceSHA, SourceManifestSHA256: b.SourceManifestSHA256, OwnerPublicKeyHex: hex.EncodeToString(key.Public().(ed25519.PublicKey)), OwnerReceiptSHA256: c18CatalogSHA(raw), C1809ReceiptSHA256: base.C1809ReceiptSHA256}
		rootRaw, e := c18CatalogBytes(root)
		if e != nil {
			t.Fatal(e)
		}
		bound := b
		bound.ContextSHA256 = c18CatalogSHA(rootRaw)
		return rootRaw, raw, bound
	}
	for _, p := range []struct {
		name, want string
		mutate     func(*c18CatalogOwnerPayload)
	}{
		{"not_dedicated", "fixture owner scope", func(p *c18CatalogOwnerPayload) { p.DedicatedNewFixtureCluster = false }},
		{"no_fixture_superuser_attestation", "fixture owner scope", func(p *c18CatalogOwnerPayload) { p.FixtureSuperuser = false }},
		{"unapproved_role_scope", "fixture owner scope", func(p *c18CatalogOwnerPayload) { p.TransactionOnlyNOLOGINRoles = 3 }},
		{"unapproved_system_schema_probe", "fixture owner scope", func(p *c18CatalogOwnerPayload) { p.InformationSchemaRollbackProbe = false }},
		{"unapproved_pg_catalog_probe", "fixture owner scope", func(p *c18CatalogOwnerPayload) { p.PGCatalogFunctionRollbackProbe = false }},
		{"wrong_attempt", "fixture owner source/C18-09 binding", func(p *c18CatalogOwnerPayload) { p.AttemptID = "other" }},
		{"expired", "fixture owner expired/not-yet-issued", func(p *c18CatalogOwnerPayload) { p.ExpiresUnixSeconds = 1700000199 }},
	} {
		t.Run(p.name, func(t *testing.T) {
			root, receipt, bound := encode(base)
			if _, e := c18CatalogValidateOwner(root, receipt, bound, now); e != nil {
				t.Fatal("same-source owner positive:", e)
			}
			changed := base
			p.mutate(&changed)
			root, receipt, bound = encode(changed)
			if _, e := c18CatalogValidateOwner(root, receipt, bound, now); e == nil || e.Error() != p.want {
				t.Fatalf("owner gate: got %v, want %s", e, p.want)
			}
		})
	}
	t.Run("missing_trust_inputs", func(t *testing.T) {
		root, receipt, bound := encode(base)
		if _, e := c18CatalogValidateOwner(root, receipt, bound, now); e != nil {
			t.Fatal(e)
		}
		if _, e := c18CatalogValidateOwner(nil, receipt, bound, now); e == nil || e.Error() != "missing/mismatched external root context" {
			t.Fatalf("missing context gate: %v", e)
		}
		if _, e := c18CatalogValidateOwner(root, nil, bound, now); e == nil || e.Error() != "missing/mismatched fixture owner receipt" {
			t.Fatalf("missing receipt gate: %v", e)
		}
	})
}

// pg_depend omits dependencies on pinned initdb objects; neither an absent edge,
// a low OID, an owner name nor pg_catalog membership proves bootstrap origin.
// PG16: https://www.postgresql.org/docs/16/catalog-pg-depend.html . The independent
// C18-09 producer must bind a verified distribution/initdb evidence and approved
// extension SQL/library artifacts. This collector does NOT generate that proof.
type c18CatalogExtensionArtifact struct {
	Version       string `json:"version"`
	SQLSHA256     string `json:"sql_sha256"`
	LibrarySHA256 string `json:"library_sha256"`
}
type c18CatalogExternalObject struct {
	Class          string        `json:"class"`
	Identity       string        `json:"identity"`
	Schema         *string       `json:"schema"`
	OriginKind     string        `json:"origin_kind"`
	OriginName     string        `json:"origin_name"`
	OriginVersion  string        `json:"origin_version"`
	ArtifactSHA256 string        `json:"artifact_sha256"`
	Definition     c18CatalogRow `json:"definition"`
}
type c18CatalogExternalReference struct {
	Profile c18CatalogBootstrapProfile `json:"required_bootstrap_profile"`

	Kind                 string                                 `json:"kind"`
	PGMajor              int                                    `json:"pg_major"`
	C1809ReceiptSHA256   string                                 `json:"c18_09_receipt_sha256"`
	DistributionSHA256   string                                 `json:"distribution_sha256"`
	InitdbEvidenceSHA256 string                                 `json:"initdb_evidence_sha256"`
	Extensions           map[string]c18CatalogExtensionArtifact `json:"extensions"`
	Objects              []c18CatalogExternalObject             `json:"objects"`
}

func c18CatalogExternalQuery(class string) (c18CatalogQuery, error) {
	// Unsupported external classes are refused even if a manifest lists them.
	// This avoids claiming semantic coverage from a name/OID or incomplete row.
	switch class {
	case "pg_proc":
		for _, q := range c18CatalogQueries {
			if q.Name == "functions" {
				q.SQL = strings.Replace(q.SQL, "'definition',pg_get_functiondef(p.oid)", "'definition',CASE WHEN p.prokind='a' THEN NULL ELSE pg_get_functiondef(p.oid) END", 1)
				q.SQL = strings.Replace(q.SQL, "'binary',p.probin)", "'binary',p.probin,'aggregate',"+c18CatalogAggregateExpression+")", 1)
				q.SQL = strings.Replace(q.SQL, "WHERE n.nspname='public'", "WHERE p.oid=$1", 1)
				q.Keys += " aggregate"
				return q, nil
			}
		}
	case "pg_type", "pg_operator", "pg_opclass":
		collection, alias := "types", "t"
		if class == "pg_operator" {
			collection, alias = "operators", "o"
		}
		if class == "pg_opclass" {
			collection, alias = "opclasses", "o"
		}
		for _, q := range c18CatalogQueries {
			if q.Name == collection {
				q.SQL = strings.Replace(q.SQL, "WHERE n.nspname='public'", "WHERE "+alias+".oid=$1", 1)
				return q, nil
			}
		}
	case "pg_opfamily":
		var operators, procedures c18CatalogQuery
		for _, q := range c18CatalogQueries {
			if q.Name == "am_operators" {
				operators = q
			}
			if q.Name == "am_procedures" {
				procedures = q
			}
		}
		operators.SQL = strings.Replace(operators.SQL, "WHERE n.nspname='public'", "WHERE f.oid=$1", 1)
		procedures.SQL = strings.Replace(procedures.SQL, "WHERE n.nspname='public'", "WHERE f.oid=$1", 1)
		return c18CatalogQuery{"external_opfamily", "identity owner operators procedures", `SELECT jsonb_build_object('identity',(pg_identify_object('pg_opfamily'::regclass,o.oid,0)).identity,'owner',pg_get_userbyid(o.opfowner),'operators',(SELECT jsonb_agg(v.body::jsonb ORDER BY v.body COLLATE "C") FROM (` + operators.SQL + `) v(body)),'procedures',(SELECT jsonb_agg(v.body::jsonb ORDER BY v.body COLLATE "C") FROM (` + procedures.SQL + `) v(body)))::text FROM pg_opfamily o WHERE o.oid=$1`}, nil
	case "pg_am":
		return c18CatalogQuery{"external_access_method", "name kind handler handler_definition handler_owner handler_acl", `SELECT jsonb_build_object('name',a.amname,'kind',a.amtype,'handler',a.amhandler::regprocedure::text,'handler_definition',pg_get_functiondef(p.oid),'handler_owner',pg_get_userbyid(p.proowner),'handler_acl',p.proacl::text)::text FROM pg_am a JOIN pg_proc p ON p.oid=a.amhandler WHERE a.oid=$1`}, nil
	case "pg_namespace":
		return c18CatalogQuery{"external_namespace", "name owner acl", `SELECT jsonb_build_object('name',nspname,'owner',pg_get_userbyid(nspowner),'acl',nspacl::text)::text FROM pg_namespace WHERE oid=$1`}, nil
	case "pg_language":
		return c18CatalogQuery{"external_language", "name owner procedural trusted handler inline validator acl handler_definition inline_definition validator_definition handler_owner handler_acl inline_owner inline_acl validator_owner validator_acl", `SELECT jsonb_build_object('name',l.lanname,'owner',pg_get_userbyid(l.lanowner),'procedural',l.lanispl,'trusted',l.lanpltrusted,'handler',l.lanplcallfoid::regprocedure::text,'inline',l.laninline::regprocedure::text,'validator',l.lanvalidator::regprocedure::text,'acl',l.lanacl::text,'handler_definition',CASE WHEN h.oid IS NULL THEN NULL ELSE pg_get_functiondef(h.oid) END,'inline_definition',CASE WHEN i.oid IS NULL THEN NULL ELSE pg_get_functiondef(i.oid) END,'validator_definition',CASE WHEN v.oid IS NULL THEN NULL ELSE pg_get_functiondef(v.oid) END,'handler_owner',pg_get_userbyid(h.proowner),'handler_acl',h.proacl::text,'inline_owner',pg_get_userbyid(i.proowner),'inline_acl',i.proacl::text,'validator_owner',pg_get_userbyid(v.proowner),'validator_acl',v.proacl::text)::text FROM pg_language l LEFT JOIN pg_proc h ON h.oid=l.lanplcallfoid LEFT JOIN pg_proc i ON i.oid=l.laninline LEFT JOIN pg_proc v ON v.oid=l.lanvalidator WHERE l.oid=$1`}, nil
	case "pg_collation":
		for _, q := range c18CatalogQueries {
			if q.Name == "collations" {
				at := strings.Index(q.SQL, " WHERE n.nspname='public'")
				if at < 0 {
					return q, fmt.Errorf("external collation serializer unavailable")
				}
				q.SQL = q.SQL[:at] + " WHERE c.oid=$1"
				return q, nil
			}
		}
	}
	return c18CatalogQuery{}, fmt.Errorf("unsupported external definition class: %s", class)
}
func c18CatalogValidateExternalReference(ref *c18CatalogExternalReference, c1809 string) error {
	if ref == nil || ref.Kind != "c18_pg16_external_provenance_reference_v2" || ref.PGMajor != 16 || ref.C1809ReceiptSHA256 != c1809 || !c18CatalogHashValid(c1809, 32) || !c18CatalogHashValid(ref.DistributionSHA256, 32) || !c18CatalogHashValid(ref.InitdbEvidenceSHA256, 32) {
		return fmt.Errorf("external provenance source evidence missing")
	}
	if len(ref.Extensions) != 3 {
		return fmt.Errorf("external provenance extension artifact set")
	}
	for _, name := range []string{"plpgsql", "pgcrypto", "pg_trgm"} {
		a, ok := ref.Extensions[name]
		if !ok || a.Version == "" || !c18CatalogHashValid(a.SQLSHA256, 32) || !c18CatalogHashValid(a.LibrarySHA256, 32) {
			return fmt.Errorf("external provenance extension artifact")
		}
	}
	if len(ref.Objects) > 20000 {
		return fmt.Errorf("external provenance object set")
	}
	seen := map[string]bool{}
	for _, o := range ref.Objects {
		key := o.Class + "\x00" + o.Identity
		if o.Identity == "" || seen[key] {
			return fmt.Errorf("external provenance duplicate/empty identity")
		}
		seen[key] = true
		spec, e := c18CatalogExternalQuery(o.Class)
		if e != nil {
			return e
		}
		if !c18CatalogExactKeys(o.Definition, spec.Keys) {
			return fmt.Errorf("external provenance definition shape")
		}
		if e = c18CatalogValidateExternalDefinition(o.Class, o.Definition); e != nil {
			return e
		}
		switch o.OriginKind {
		case "verified_initdb_bootstrap":
			if o.OriginName != "postgresql" || o.OriginVersion != "16" || o.ArtifactSHA256 != ref.DistributionSHA256 {
				return fmt.Errorf("external bootstrap artifact binding")
			}
		case "verified_extension_member":
			a, ok := ref.Extensions[o.OriginName]
			raw, e := c18CatalogBytes(a)
			if !ok || e != nil || o.OriginVersion != a.Version || o.ArtifactSHA256 != c18CatalogSHA(raw) {
				return fmt.Errorf("external extension artifact binding")
			}
		default:
			return fmt.Errorf("unapproved external provenance origin")
		}
	}
	return c18CatalogValidateBootstrapProfile(ref)
}
func c18CatalogLoadExternalReference(t *testing.T, owner c18CatalogOwnerReceipt) *c18CatalogExternalReference {
	t.Helper()
	raw, e := os.ReadFile(os.Getenv("TABMAIL_C18_CATALOG_EXTERNAL_REFERENCE"))
	if e != nil || len(raw) > 16<<20 || c18CatalogSHA(raw) != owner.Payload.ExternalReferenceSHA256 {
		t.Fatal("signed external provenance reference missing/mismatched")
	}
	var ref c18CatalogExternalReference
	if e = c18CatalogDecode(raw, &ref); e != nil {
		t.Fatal("external provenance reference grammar")
	}
	canonical, e := c18CatalogBytes(ref)
	if e != nil || !bytes.Equal(canonical, raw) {
		t.Fatal("external provenance reference must be canonical")
	}
	if e = c18CatalogValidateExternalReference(&ref, owner.Payload.C1809ReceiptSHA256); e != nil {
		t.Fatal(e)
	}
	return &ref
}
func c18CatalogApproveExternal(ref *c18CatalogExternalReference, class, identity string, schema *string, definition c18CatalogRow, extension, version string) (c18CatalogExternalObject, error) {
	if ref == nil {
		return c18CatalogExternalObject{}, fmt.Errorf("external provenance reference required")
	}
	for _, expected := range ref.Objects {
		if expected.Class != class || expected.Identity != identity {
			continue
		}
		if !reflect.DeepEqual(expected.Schema, schema) {
			return expected, fmt.Errorf("external provenance schema mismatch")
		}
		if expected.OriginKind == "verified_initdb_bootstrap" {
			if extension != "" {
				return expected, fmt.Errorf("external provenance membership mismatch")
			}
		} else if expected.OriginKind != "verified_extension_member" || expected.OriginName != extension || expected.OriginVersion != version {
			return expected, fmt.Errorf("external provenance membership mismatch")
		}
		a, e := c18CatalogBytes(expected.Definition)
		if e != nil {
			return expected, e
		}
		b, e := c18CatalogBytes(definition)
		if e != nil {
			return expected, e
		}
		if !bytes.Equal(a, b) {
			return expected, fmt.Errorf("external semantic definition drift")
		}
		return expected, nil
	}
	return c18CatalogExternalObject{}, fmt.Errorf("unapproved external dependency object")
}
func c18CatalogOwnerTTL(issued, expires, now int64) error {
	const minimum int64 = 946684800  // 2000-01-01; older/negative epochs are not receipts.
	const maximum int64 = 4102444800 // 2100-01-01; explicitly reviewed bounded horizon.
	if issued < minimum || issued > maximum || expires < minimum || expires > maximum || now < minimum || now > maximum {
		return fmt.Errorf("fixture owner time range")
	}
	// All operands are bounded above before arithmetic. No signed-int subtraction
	// on attacker-provided extremes, and issued+600 cannot overflow in this domain.
	if issued > now || expires <= now || expires <= issued || expires > issued+600 {
		return fmt.Errorf("fixture owner expired/not-yet-issued")
	}
	return nil
}
func TestR5Current18FullCatalogOwnerTTLBounds(t *testing.T) {
	for _, p := range []struct {
		name                 string
		issued, expires, now int64
		want                 string
	}{
		{"exact_600", 1700000000, 1700000600, 1700000001, ""},
		{"over_600", 1700000000, 1700000601, 1700000001, "fixture owner expired/not-yet-issued"},
		{"negative_issue_overflow", -9223372036854775808, 1700000300, 1700000200, "fixture owner time range"},
		{"both_extremes", -9223372036854775808, 9223372036854775807, 1700000200, "fixture owner time range"},
		{"max_issue", 9223372036854775807, 1700000300, 1700000200, "fixture owner time range"},
		{"negative_expiry", 1700000000, -1, 1700000200, "fixture owner time range"},
		{"minimum_boundary", 946684800, 946685400, 946684801, ""},
		{"maximum_boundary", 4102444700, 4102444800, 4102444701, ""},
		{"beyond_horizon", 4102444700, 4102444801, 4102444701, "fixture owner time range"},
	} {
		t.Run(p.name, func(t *testing.T) {
			if e := c18CatalogOwnerTTL(1700000000, 1700000600, 1700000001); e != nil {
				t.Fatal("TTL positive:", e)
			}
			e := c18CatalogOwnerTTL(p.issued, p.expires, p.now)
			if p.want == "" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil || e.Error() != p.want {
				t.Fatalf("TTL gate: got %v, want %s", e, p.want)
			}
		})
	}
}

func TestR5Current18FullCatalogExternalProvenanceGate(t *testing.T) {
	schema := "pg_catalog"
	makeReference := func() *c18CatalogExternalReference {
		q, e := c18CatalogExternalQuery("pg_proc")
		if e != nil {
			t.Fatal(e)
		}
		definition := c18CatalogRow{}
		for _, k := range strings.Fields(q.Keys) {
			definition[k] = nil
		}
		definition["definition"] = "synthetic SQL-standard body WHERE true"
		definition["kind"] = "f"
		definition["aggregate"] = nil
		definition["cost"] = json.Number("1")
		definition["volatility"] = "v"
		ref := &c18CatalogExternalReference{Kind: "c18_pg16_external_provenance_reference_v2", PGMajor: 16, C1809ReceiptSHA256: strings.Repeat("1", 64), DistributionSHA256: strings.Repeat("2", 64), InitdbEvidenceSHA256: strings.Repeat("3", 64), Extensions: map[string]c18CatalogExtensionArtifact{}, Objects: []c18CatalogExternalObject{{Class: "pg_proc", Identity: "pg_catalog.gen_random_uuid()", Schema: &schema, OriginKind: "verified_initdb_bootstrap", OriginName: "postgresql", OriginVersion: "16", ArtifactSHA256: strings.Repeat("2", 64), Definition: definition}}}
		ref.Profile = c18CatalogBootstrapProfile{ID: c18CatalogRequiredProfile, FieldsProtocol: c18CatalogReferenceFieldsProtocol, RoutineKinds: []string{"a", "f", "p", "w"}, RequiredBootstrap: []c18CatalogObjectKey{{Class: "pg_proc", Identity: "pg_catalog.gen_random_uuid()", Schema: &schema}}}
		for _, name := range []string{"plpgsql", "pgcrypto", "pg_trgm"} {
			ref.Extensions[name] = c18CatalogExtensionArtifact{"synthetic-version", strings.Repeat("4", 64), strings.Repeat("5", 64)}
		}
		if e := c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e != nil {
			t.Fatal("synthetic provenance grammar positive:", e)
		}
		o := ref.Objects[0]
		if _, e := c18CatalogApproveExternal(ref, o.Class, o.Identity, o.Schema, o.Definition, "", ""); e != nil {
			t.Fatal("same-source semantic positive:", e)
		}
		return ref
	}
	t.Run("system_schema_name_is_not_provenance", func(t *testing.T) {
		ref := makeReference()
		o := ref.Objects[0]
		if _, e := c18CatalogApproveExternal(ref, "pg_proc", "pg_catalog.unapproved_custom()", o.Schema, o.Definition, "", ""); e == nil || e.Error() != "unapproved external dependency object" {
			t.Fatalf("custom-system-object gate: %v", e)
		}
	})
	t.Run("same_identity_changed_definition", func(t *testing.T) {
		ref := makeReference()
		o := ref.Objects[0]
		raw, e := c18CatalogBytes(o.Definition)
		if e != nil {
			t.Fatal(e)
		}
		var changed c18CatalogRow
		if e = c18CatalogDecode(raw, &changed); e != nil {
			t.Fatal(e)
		}
		changed["definition"] = "synthetic SQL-standard body WHERE false"
		if _, e = c18CatalogApproveExternal(ref, o.Class, o.Identity, o.Schema, changed, "", ""); e == nil || e.Error() != "external semantic definition drift" {
			t.Fatalf("same-edge semantic gate: %v", e)
		}
	})
	t.Run("extension_membership_not_bootstrap", func(t *testing.T) {
		ref := makeReference()
		o := ref.Objects[0]
		if _, e := c18CatalogApproveExternal(ref, o.Class, o.Identity, o.Schema, o.Definition, "plpgsql", "synthetic-version"); e == nil || e.Error() != "external provenance membership mismatch" {
			t.Fatalf("origin gate: %v", e)
		}
	})
	t.Run("approved_extension_then_wrong_membership", func(t *testing.T) {
		ref := makeReference()
		o := &ref.Objects[0]
		artifact := ref.Extensions["plpgsql"]
		raw, e := c18CatalogBytes(artifact)
		if e != nil {
			t.Fatal(e)
		}
		o.OriginKind = "verified_extension_member"
		o.OriginName = "plpgsql"
		o.OriginVersion = artifact.Version
		o.ArtifactSHA256 = c18CatalogSHA(raw)
		if e = c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e != nil {
			t.Fatal("extension source positive:", e)
		}
		if _, e = c18CatalogApproveExternal(ref, o.Class, o.Identity, o.Schema, o.Definition, "plpgsql", artifact.Version); e != nil {
			t.Fatal("extension live-membership positive:", e)
		}
		if _, e = c18CatalogApproveExternal(ref, o.Class, o.Identity, o.Schema, o.Definition, "pg_trgm", artifact.Version); e == nil || e.Error() != "external provenance membership mismatch" {
			t.Fatalf("extension membership gate: %v", e)
		}
	})
	t.Run("unapproved_origin", func(t *testing.T) {
		ref := makeReference()
		ref.Objects[0].OriginKind = "schema_name_only"
		if e := c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e == nil || e.Error() != "unapproved external provenance origin" {
			t.Fatalf("name-only origin gate: %v", e)
		}
	})
	t.Run("missing_initdb_source", func(t *testing.T) {
		ref := makeReference()
		ref.InitdbEvidenceSHA256 = ""
		if e := c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e == nil || e.Error() != "external provenance source evidence missing" {
			t.Fatalf("bootstrap evidence gate: %v", e)
		}
	})
}

// Full state admission never grants pg_catalog a semantic exemption. The
// collector first checks local ownership, then fills external_objects through
// signed provenance comparison, and finally invokes this full entry point.
func c18CatalogValidateState(c c18Catalog) error {
	if e := c18CatalogValidateLocalState(c); e != nil {
		return e
	}
	return c18CatalogValidateExternalCoverage(c)
}
func TestR5Current18FullCatalogSystemSchemaNeedsFullCoverage(t *testing.T) {
	for _, p := range []struct {
		name, want string
		mutate     func(c18Catalog)
	}{
		{"missing_pg_catalog_definition", "required system definition missing", func(c c18Catalog) { c["external_objects"] = []c18CatalogRow{} }},
		{"custom_pg_catalog_identity", "unapproved external dependency object", func(c c18Catalog) { c["dependencies"][0]["reference"] = "pg_catalog.c18_external()" }},
		{"empty_function_definition", "external coverage function definition", func(c c18Catalog) {
			definition := c["external_objects"][0]["definition"].(c18CatalogRow)
			definition["definition"] = ""
		}},
		{"schema_name_is_not_origin", "unapproved external provenance origin", func(c c18Catalog) { c["external_objects"][0]["origin_kind"] = "pg_catalog_name" }},
	} {
		t.Run(p.name, func(t *testing.T) {
			c := c18CatalogSyntheticState()
			if e := c18CatalogValidateState(c); e != nil {
				t.Fatal("full state positive:", e)
			}
			p.mutate(c)
			if e := c18CatalogValidateState(c); e == nil || e.Error() != p.want {
				t.Fatalf("full external coverage gate: got %v, want %s", e, p.want)
			}
		})
	}
}

const c18CatalogRequiredProfile = "pg16_current18_all_catalog_routines_and_live_references_v1"
const c18CatalogReferenceFieldsProtocol = "public_catalog_fields_v1_defaults_generated_bodies_via_all_catalog_routines"

type c18CatalogObjectKey struct {
	Class    string  `json:"class"`
	Identity string  `json:"identity"`
	Schema   *string `json:"schema"`
}
type c18CatalogBootstrapProfile struct {
	ID                string                `json:"id"`
	FieldsProtocol    string                `json:"fields_protocol"`
	RoutineKinds      []string              `json:"routine_kinds"`
	RequiredBootstrap []c18CatalogObjectKey `json:"required_bootstrap"`
}
type c18CatalogRequiredObject struct {
	class, identity string
	schema          *string
	oid             int64
	reasons         []string
}

func c18CatalogValidateBootstrapProfile(ref *c18CatalogExternalReference) error {
	p := ref.Profile
	if p.ID != c18CatalogRequiredProfile || p.FieldsProtocol != c18CatalogReferenceFieldsProtocol || !reflect.DeepEqual(p.RoutineKinds, []string{"a", "f", "p", "w"}) {
		return fmt.Errorf("unknown required bootstrap profile")
	}
	if len(p.RequiredBootstrap) == 0 {
		return fmt.Errorf("required bootstrap builtin missing: pg_catalog.gen_random_uuid()")
	}
	if len(p.RequiredBootstrap) > 20000 {
		return fmt.Errorf("required bootstrap profile oversized")
	}
	approved := map[string]bool{}
	for _, o := range ref.Objects {
		approved[o.Class+"\x00"+o.Identity] = true
	}
	seen := map[string]bool{}
	for _, key := range p.RequiredBootstrap {
		k := key.Class + "\x00" + key.Identity
		if key.Class != "pg_proc" || key.Schema == nil || *key.Schema != "pg_catalog" || key.Identity == "" || seen[k] {
			return fmt.Errorf("required bootstrap identity shape")
		}
		if !approved[k] {
			return fmt.Errorf("approved required definition missing")
		}
		seen[k] = true
	}
	// A known current18 DEFAULT owner is an explicit profile witness; this is not
	// the inventory itself and is never used as an OID/namespace trust shortcut.
	if !seen["pg_proc\x00pg_catalog.gen_random_uuid()"] {
		return fmt.Errorf("required bootstrap builtin missing: pg_catalog.gen_random_uuid()")
	}
	return nil
}

const c18CatalogAggregateKeys = "kind direct_args transition final combine serialize deserialize moving_transition moving_inverse moving_final final_extra moving_final_extra final_modify moving_final_modify sort_operator transition_type transition_space moving_type moving_space initial moving_initial"
const c18CatalogAggregateExpression = `(SELECT jsonb_build_object('kind',a.aggkind,'direct_args',a.aggnumdirectargs,'transition',a.aggtransfn::regprocedure::text,'final',a.aggfinalfn::regprocedure::text,'combine',a.aggcombinefn::regprocedure::text,'serialize',a.aggserialfn::regprocedure::text,'deserialize',a.aggdeserialfn::regprocedure::text,'moving_transition',a.aggmtransfn::regprocedure::text,'moving_inverse',a.aggminvtransfn::regprocedure::text,'moving_final',a.aggmfinalfn::regprocedure::text,'final_extra',a.aggfinalextra,'moving_final_extra',a.aggmfinalextra,'final_modify',a.aggfinalmodify,'moving_final_modify',a.aggmfinalmodify,'sort_operator',a.aggsortop::regoperator::text,'transition_type',a.aggtranstype::regtype::text,'transition_space',a.aggtransspace,'moving_type',a.aggmtranstype::regtype::text,'moving_space',a.aggmtransspace,'initial',a.agginitval,'moving_initial',a.aggminitval) FROM pg_aggregate a WHERE a.aggfnoid=p.oid)`

func c18CatalogValidateExternalDefinition(class string, d c18CatalogRow) error {
	switch class {
	case "pg_proc":
		switch d["kind"] {
		case "f", "p", "w":
			if body, ok := d["definition"].(string); !ok || body == "" {
				return fmt.Errorf("external coverage function definition")
			}
			if d["aggregate"] != nil {
				return fmt.Errorf("unexpected aggregate definition")
			}
		case "a":
			raw, e := c18CatalogBytes(d["aggregate"])
			if e != nil {
				return e
			}
			var a c18CatalogRow
			if e = c18CatalogDecode(raw, &a); e != nil || !c18CatalogExactKeys(a, c18CatalogAggregateKeys) || d["definition"] != nil {
				return fmt.Errorf("aggregate system definition incomplete")
			}
		default:
			return fmt.Errorf("unsupported required system routine kind")
		}
	case "pg_type":
		if d["defined"] != true || (d["kind"] != "b" && d["kind"] != "p") {
			return fmt.Errorf("unsupported required system type kind")
		}
	}
	return nil
}

// This query deliberately has NO pg_depend join and NO OID threshold, language
// filter, prokind exclusion or caller-supplied name predicate. Every actual PG16
// pg_catalog routine is independently enumerated, including pinned builtins.
const c18CatalogBootstrapInventorySQL = `SELECT 'pg_proc',p.oid::bigint,(pg_identify_object('pg_proc'::regclass,p.oid,0)).identity,n.nspname::text,'bootstrap_routine:'||p.prokind::text FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='pg_catalog' ORDER BY 3`

func c18CatalogReferenceInventorySQL() string {
	// Actual scalar/array catalog references restore edges PostgreSQL intentionally
	// omits for pinned objects. Domain/range/composite *external* types remain
	// unsupported instead of pretending base-type serialization covers them.
	return strings.Replace(c18CatalogObjectScopeCTE, "WITH public_rel AS", "WITH RECURSIVE public_rel AS", 1) + `, field_seed(classid,objid,reason) AS (
 SELECT 'pg_type'::regclass,a.atttypid,'pg_attribute.atttypid' FROM pg_attribute a JOIN public_rel r ON r.oid=a.attrelid WHERE a.attnum>0 AND NOT a.attisdropped
 UNION SELECT 'pg_collation'::regclass,a.attcollation,'pg_attribute.attcollation' FROM pg_attribute a JOIN public_rel r ON r.oid=a.attrelid WHERE a.attnum>0
 UNION SELECT 'pg_proc'::regclass,t.tgfoid,'pg_trigger.tgfoid' FROM pg_trigger t JOIN public_rel r ON r.oid=t.tgrelid
 UNION SELECT 'pg_language'::regclass,p.prolang,'pg_proc.prolang' FROM pg_proc p WHERE p.pronamespace='public'::regnamespace
 UNION SELECT 'pg_type'::regclass,p.prorettype,'pg_proc.prorettype' FROM pg_proc p WHERE p.pronamespace='public'::regnamespace
 UNION SELECT 'pg_type'::regclass,x,'pg_proc.argument_types' FROM pg_proc p CROSS JOIN LATERAL unnest(COALESCE(p.proallargtypes,p.proargtypes::oid[])) u(x) WHERE p.pronamespace='public'::regnamespace
 UNION SELECT 'pg_am'::regclass,c.relam,'pg_class.relam' FROM pg_class c JOIN public_rel r ON r.oid=c.oid
 UNION SELECT 'pg_opclass'::regclass,x,'pg_index.indclass' FROM pg_index i JOIN public_rel r ON r.oid=i.indrelid CROSS JOIN LATERAL unnest(i.indclass::oid[]) u(x)
 UNION SELECT 'pg_collation'::regclass,x,'pg_index.indcollation' FROM pg_index i JOIN public_rel r ON r.oid=i.indrelid CROSS JOIN LATERAL unnest(i.indcollation::oid[]) u(x)
 UNION SELECT 'pg_operator'::regclass,x,'pg_constraint.equality_exclusion_operators' FROM pg_constraint c CROSS JOIN LATERAL unnest(COALESCE(c.conpfeqop,'{}'::oid[])||COALESCE(c.conppeqop,'{}'::oid[])||COALESCE(c.conffeqop,'{}'::oid[])||COALESCE(c.conexclop,'{}'::oid[])) u(x) WHERE c.connamespace='public'::regnamespace
 UNION SELECT 'pg_type'::regclass,s.seqtypid,'pg_sequence.seqtypid' FROM pg_sequence s JOIN public_rel r ON r.oid=s.seqrelid
 UNION SELECT 'pg_type'::regclass,t.oid,'public_type_definition' FROM pg_type t WHERE t.typnamespace='public'::regnamespace
 UNION SELECT 'pg_opclass'::regclass,o.oid,'public_opclass_definition' FROM pg_opclass o WHERE o.opcnamespace='public'::regnamespace
 UNION SELECT 'pg_opfamily'::regclass,f.oid,'public_opfamily_definition' FROM pg_opfamily f WHERE f.opfnamespace='public'::regnamespace
), used_opclasses AS (
 SELECT o.* FROM pg_opclass o WHERE o.oid IN(SELECT objid FROM field_seed WHERE classid='pg_opclass'::regclass)
), used_families AS (
 SELECT opcfamily AS oid FROM used_opclasses UNION SELECT objid FROM field_seed WHERE classid='pg_opfamily'::regclass
), field_refs(classid,objid,reason) AS (
 SELECT * FROM field_seed
 UNION SELECT 'pg_opfamily'::regclass,oid,'pg_opclass.opcfamily' FROM used_families
 UNION SELECT 'pg_type'::regclass,x,'pg_opclass.input_key_types' FROM used_opclasses o CROSS JOIN LATERAL unnest(ARRAY[o.opcintype,o.opckeytype]) u(x)
 UNION SELECT 'pg_operator'::regclass,a.amopopr,'pg_amop.amopopr' FROM pg_amop a JOIN used_families f ON f.oid=a.amopfamily
 UNION SELECT 'pg_proc'::regclass,a.amproc::oid,'pg_amproc.amproc' FROM pg_amproc a JOIN used_families f ON f.oid=a.amprocfamily
 UNION SELECT 'pg_type'::regclass,x,'pg_amop.operand_types' FROM pg_amop a JOIN used_families f ON f.oid=a.amopfamily CROSS JOIN LATERAL unnest(ARRAY[a.amoplefttype,a.amoprighttype]) u(x)
 UNION SELECT 'pg_type'::regclass,x,'pg_amproc.operand_types' FROM pg_amproc a JOIN used_families f ON f.oid=a.amprocfamily CROSS JOIN LATERAL unnest(ARRAY[a.amproclefttype,a.amprocrighttype]) u(x)
), type_closure(oid) AS (
 SELECT objid FROM field_refs WHERE classid='pg_type'::regclass AND objid<>0
 UNION SELECT v.oid FROM type_closure c JOIN pg_type t ON t.oid=c.oid CROSS JOIN LATERAL (VALUES(t.typbasetype),(t.typelem)) v(oid) WHERE v.oid<>0
), required_fields(classid,objid,reason) AS (
 SELECT * FROM field_refs
 UNION SELECT 'pg_type'::regclass,oid,'type_base_element_closure' FROM type_closure
 UNION SELECT 'pg_proc'::regclass,x,'pg_type.io_typmod_analyze_subscript' FROM pg_type t JOIN type_closure c ON c.oid=t.oid CROSS JOIN LATERAL unnest(ARRAY[t.typinput::oid,t.typoutput::oid,t.typreceive::oid,t.typsend::oid,t.typmodin::oid,t.typmodout::oid,t.typanalyze::oid,t.typsubscript::oid]) u(x)
 UNION SELECT 'pg_proc'::regclass,a.amhandler::oid,'pg_am.amhandler' FROM pg_am a WHERE a.oid IN(SELECT objid FROM field_refs WHERE classid='pg_am'::regclass)
 UNION SELECT 'pg_proc'::regclass,x,'pg_operator.implementation_selectivity' FROM pg_operator o CROSS JOIN LATERAL unnest(ARRAY[o.oprcode::oid,o.oprrest::oid,o.oprjoin::oid]) u(x) WHERE o.oid IN(SELECT objid FROM field_refs WHERE classid='pg_operator'::regclass)
) SELECT DISTINCT r.classid::regclass::text,r.objid::bigint,o.identity,l.owner_schema,r.reason FROM required_fields r LEFT JOIN object_scope s ON s.classid=r.classid AND s.objid=r.objid LEFT JOIN object_locations l ON l.classid=r.classid AND l.objid=r.objid CROSS JOIN LATERAL pg_identify_object(r.classid,r.objid,0) o WHERE r.objid<>0 AND s.objid IS NULL ORDER BY 1,3,5`
}
func c18CatalogReadRequired(ctx context.Context, q c18CatalogQuerier, sql string) ([]c18CatalogRequiredObject, error) {
	rows, e := q.Query(ctx, sql)
	if e != nil {
		return nil, e
	}
	out := []c18CatalogRequiredObject{}
	for rows.Next() {
		var v c18CatalogRequiredObject
		var reason string
		if e = rows.Scan(&v.class, &v.oid, &v.identity, &v.schema, &reason); e != nil {
			rows.Close()
			return nil, e
		}
		v.reasons = []string{reason}
		out = append(out, v)
		if len(out) > 50000 {
			rows.Close()
			return nil, fmt.Errorf("required system inventory resource ceiling")
		}
	}
	e = rows.Err()
	rows.Close()
	return out, e
}
func c18CatalogBuildRequired(ref *c18CatalogExternalReference, bootstrap, fields, edges []c18CatalogRequiredObject) ([]c18CatalogRequiredObject, error) {
	if ref == nil {
		return nil, fmt.Errorf("required bootstrap profile missing")
	}
	if e := c18CatalogValidateBootstrapProfile(ref); e != nil {
		return nil, e
	}
	approved := map[string]bool{}
	for _, o := range ref.Objects {
		approved[o.Class+"\x00"+o.Identity] = true
	}
	declared := map[string]bool{}
	for _, k := range ref.Profile.RequiredBootstrap {
		declared[k.Class+"\x00"+k.Identity] = true
	}
	actual := map[string]bool{}
	for _, v := range bootstrap {
		key := v.class + "\x00" + v.identity
		if actual[key] || v.class != "pg_proc" || v.schema == nil || *v.schema != "pg_catalog" || len(v.reasons) != 1 {
			return nil, fmt.Errorf("actual bootstrap inventory shape")
		}
		switch v.reasons[0] {
		case "bootstrap_routine:a", "bootstrap_routine:f", "bootstrap_routine:p", "bootstrap_routine:w":
		default:
			return nil, fmt.Errorf("unsupported required system routine kind")
		}
		if !declared[key] {
			if !approved[key] {
				return nil, fmt.Errorf("unapproved external dependency object")
			}
			return nil, fmt.Errorf("required bootstrap profile omits live routine")
		}
		actual[key] = true
	}
	for key := range declared {
		if !actual[key] {
			return nil, fmt.Errorf("required system definition missing")
		}
	}
	merged := map[string]c18CatalogRequiredObject{}
	for _, group := range [][]c18CatalogRequiredObject{bootstrap, fields, edges} {
		for _, v := range group {
			if v.schema != nil && *v.schema != "pg_catalog" && *v.schema != "pg_toast" {
				return nil, fmt.Errorf("unsupported required system schema")
			}
			key := v.class + "\x00" + v.identity
			if !approved[key] {
				return nil, fmt.Errorf("approved required definition missing")
			}
			if _, e := c18CatalogExternalQuery(v.class); e != nil {
				return nil, e
			}
			previous, exists := merged[key]
			if exists {
				if previous.oid != v.oid || !reflect.DeepEqual(previous.schema, v.schema) {
					return nil, fmt.Errorf("required identity/address conflict")
				}
				v.reasons = append(v.reasons, previous.reasons...)
			}
			if declared[key] {
				v.reasons = append(v.reasons, "signed_bootstrap_profile")
			}
			reasons := map[string]bool{}
			for _, reason := range v.reasons {
				reasons[reason] = true
			}
			v.reasons = []string{}
			for reason := range reasons {
				v.reasons = append(v.reasons, reason)
			}
			sort.Strings(v.reasons)
			merged[key] = v
		}
	}
	if len(merged) > 10000 {
		return nil, fmt.Errorf("required system inventory resource ceiling")
	}
	keys := []string{}
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []c18CatalogRequiredObject{}
	for _, key := range keys {
		out = append(out, merged[key])
	}
	return out, nil
}

func c18CatalogCollectExternal(ctx context.Context, q c18CatalogQuerier, ref *c18CatalogExternalReference, c c18Catalog) error {
	if ref == nil {
		return fmt.Errorf("external provenance reference required")
	}
	for _, r := range c["extensions"] {
		name, ok := r["name"].(string)
		a, exists := ref.Extensions[name]
		if !ok || !exists || r["version"] != a.Version {
			return fmt.Errorf("live extension version lacks approved artifact")
		}
	}
	bootstrap, e := c18CatalogReadRequired(ctx, q, c18CatalogBootstrapInventorySQL)
	if e != nil {
		return e
	}
	fields, e := c18CatalogReadRequired(ctx, q, c18CatalogReferenceInventorySQL())
	if e != nil {
		return e
	}
	edges, e := c18CatalogReadRequired(ctx, q, c18CatalogObjectScopeCTE+`, endpoints(classid,objid) AS (
 SELECT d.classid,d.objid FROM pg_depend d LEFT JOIN object_scope s ON s.classid=d.classid AND s.objid=d.objid JOIN object_scope r ON r.classid=d.refclassid AND r.objid=d.refobjid WHERE s.objid IS NULL
 UNION SELECT d.refclassid,d.refobjid FROM pg_depend d JOIN object_scope s ON s.classid=d.classid AND s.objid=d.objid LEFT JOIN object_scope r ON r.classid=d.refclassid AND r.objid=d.refobjid WHERE r.objid IS NULL
 ) SELECT e.classid::regclass::text,e.objid::bigint,o.identity,l.owner_schema,'catalog_dependency' FROM endpoints e CROSS JOIN LATERAL pg_identify_object(e.classid,e.objid,0) o LEFT JOIN object_locations l ON l.classid=e.classid AND l.objid=e.objid ORDER BY 1,3`)
	if e != nil {
		return e
	}
	required, e := c18CatalogBuildRequired(ref, bootstrap, fields, edges)
	if e != nil {
		return e
	}
	c["required_external_objects"] = []c18CatalogRow{}
	c["external_objects"] = []c18CatalogRow{}
	classes := map[string][]c18CatalogRequiredObject{}
	for _, v := range required {
		c["required_external_objects"] = append(c["required_external_objects"], c18CatalogRow{"class": v.class, "identity": v.identity, "schema": v.schema, "profile": ref.Profile.ID, "required_by": v.reasons})
		classes[v.class] = append(classes[v.class], v)
	}
	names := []string{}
	for class := range classes {
		names = append(names, class)
	}
	sort.Strings(names)
	// Separate live-definition reads, batched per class. The required identities
	// came from signed profile + actual catalog fields, NOT these serializer rows.
	for _, class := range names {
		spec, e := c18CatalogExternalQuery(class)
		if e != nil {
			return e
		}
		oids := []uint32{}
		byOID := map[int64]c18CatalogRequiredObject{}
		for _, v := range classes[class] {
			if v.oid <= 0 || v.oid > 4294967295 {
				return fmt.Errorf("invalid required catalog address")
			}
			oids = append(oids, uint32(v.oid))
			byOID[v.oid] = v
		}
		definitionSQL := strings.ReplaceAll(spec.SQL, "$1", "live.objid")
		sql := `SELECT live.objid::bigint,(` + definitionSQL + `),COALESCE((SELECT e.extname FROM pg_depend d JOIN pg_extension e ON e.oid=d.refobjid WHERE d.classid=$2::regclass AND d.objid=live.objid AND d.objsubid=0 AND d.refclassid='pg_extension'::regclass AND d.deptype='e'),''),COALESCE((SELECT e.extversion FROM pg_depend d JOIN pg_extension e ON e.oid=d.refobjid WHERE d.classid=$2::regclass AND d.objid=live.objid AND d.objsubid=0 AND d.refclassid='pg_extension'::regclass AND d.deptype='e'),'') FROM unnest($1::oid[]) live(objid)`
		rows, e := q.Query(ctx, sql, oids, class)
		if e != nil {
			return e
		}
		seen := map[int64]bool{}
		for rows.Next() {
			var oid int64
			var raw *string
			var extension, version string
			if e = rows.Scan(&oid, &raw, &extension, &version); e != nil {
				rows.Close()
				return e
			}
			v, ok := byOID[oid]
			if !ok || seen[oid] || raw == nil {
				rows.Close()
				return fmt.Errorf("required system definition missing")
			}
			seen[oid] = true
			if len(*raw) > 1<<20 {
				rows.Close()
				return fmt.Errorf("external definition resource ceiling")
			}
			var definition c18CatalogRow
			if e = c18CatalogDecode([]byte(*raw), &definition); e != nil {
				rows.Close()
				return e
			}
			if !c18CatalogExactKeys(definition, spec.Keys) {
				rows.Close()
				return fmt.Errorf("external live definition shape")
			}
			if e = c18CatalogValidateExternalDefinition(class, definition); e != nil {
				rows.Close()
				return e
			}
			expected, e := c18CatalogApproveExternal(ref, class, v.identity, v.schema, definition, extension, version)
			if e != nil {
				rows.Close()
				return e
			}
			c["external_objects"] = append(c["external_objects"], c18CatalogRow{"class": class, "identity": v.identity, "schema": v.schema, "origin_kind": expected.OriginKind, "origin_name": expected.OriginName, "origin_version": expected.OriginVersion, "artifact_sha256": expected.ArtifactSHA256, "definition": definition})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(seen) != len(byOID) {
			return fmt.Errorf("required system definition missing")
		}
	}
	return nil
}

func c18CatalogValidateExternalCoverage(c c18Catalog) error {
	records := map[string]c18CatalogRow{}
	for _, r := range c["external_objects"] {
		class, ok := r["class"].(string)
		if !ok {
			return fmt.Errorf("external coverage class")
		}
		identity, ok := r["identity"].(string)
		if !ok || identity == "" {
			return fmt.Errorf("external coverage identity")
		}
		spec, e := c18CatalogExternalQuery(class)
		if e != nil {
			return e
		}
		raw, e := c18CatalogBytes(r["definition"])
		if e != nil {
			return e
		}
		var d c18CatalogRow
		if e = c18CatalogDecode(raw, &d); e != nil || !c18CatalogExactKeys(d, spec.Keys) {
			return fmt.Errorf("external coverage definition shape")
		}
		if e = c18CatalogValidateExternalDefinition(class, d); e != nil {
			return e
		}
		if r["origin_kind"] != "verified_initdb_bootstrap" && r["origin_kind"] != "verified_extension_member" {
			return fmt.Errorf("unapproved external provenance origin")
		}
		hash, ok := r["artifact_sha256"].(string)
		if !ok || !c18CatalogHashValid(hash, 32) {
			return fmt.Errorf("external coverage artifact")
		}
		key := class + "\x00" + identity
		if _, ok := records[key]; ok {
			return fmt.Errorf("duplicate external coverage identity")
		}
		records[key] = r
	}
	required := map[string]bool{}
	for _, r := range c["required_external_objects"] {
		if r["profile"] != c18CatalogRequiredProfile {
			return fmt.Errorf("unknown required bootstrap profile")
		}
		class, ok := r["class"].(string)
		if !ok {
			return fmt.Errorf("required system identity shape")
		}
		identity, ok := r["identity"].(string)
		if !ok || identity == "" {
			return fmt.Errorf("required system identity shape")
		}
		key := class + "\x00" + identity
		if required[key] {
			return fmt.Errorf("duplicate required system identity")
		}
		required[key] = true
		reasonsRaw, e := c18CatalogBytes(r["required_by"])
		if e != nil {
			return e
		}
		var reasons []string
		if e = c18CatalogDecode(reasonsRaw, &reasons); e != nil || len(reasons) == 0 {
			return fmt.Errorf("required system source missing")
		}
		observed, ok := records[key]
		if !ok {
			return fmt.Errorf("required system definition missing")
		}
		a, e := c18CatalogBytes(observed["schema"])
		if e != nil {
			return e
		}
		b, e := c18CatalogBytes(r["schema"])
		if e != nil {
			return e
		}
		if !bytes.Equal(a, b) {
			return fmt.Errorf("external provenance schema mismatch")
		}
	}
	if !required["pg_proc\x00pg_catalog.gen_random_uuid()"] {
		return fmt.Errorf("required bootstrap builtin missing: pg_catalog.gen_random_uuid()")
	}
	if len(required) != len(records) {
		return fmt.Errorf("unrequired external definition record")
	}
	// Dependencies remain an additional cross-check, never the authoritative
	// required set: a zero-edge pinned builtin is required above nonetheless.
	for _, d := range c["dependencies"] {
		for _, side := range []string{"object", "reference"} {
			if d[side+"_in_scope"] == true {
				continue
			}
			class, ok := d[side+"_class"].(string)
			if !ok {
				return fmt.Errorf("external dependency class")
			}
			identity, ok := d[side].(string)
			if !ok {
				return fmt.Errorf("external dependency identity")
			}
			key := class + "\x00" + identity
			if !required[key] {
				return fmt.Errorf("unapproved external dependency object")
			}
			expectedSchema, e := c18CatalogBytes(records[key]["schema"])
			if e != nil {
				return e
			}
			observedSchema, e := c18CatalogBytes(d[side+"_schema"])
			if e != nil {
				return e
			}
			if !bytes.Equal(expectedSchema, observedSchema) {
				return fmt.Errorf("external provenance schema mismatch")
			}
		}
	}
	return nil
}

// SOURCE-only mocked live metadata: no ALTER FUNCTION on any existing builtin.
// Each mutation first passes the SAME full State shape gate; the independent
// signed-required/profile/definition comparison then rejects semantic drift.
func TestR5Current18FullCatalogPinnedBuiltinRequiredProfile(t *testing.T) {
	fixture := func() (c18Catalog, *c18CatalogExternalReference, []c18CatalogRequiredObject) {
		c := c18CatalogSyntheticState()
		// Leave an ordinary owned dependency, but NO pg_depend edge for gen_random_uuid.
		c["dependencies"][0]["reference_in_scope"] = true
		c["dependencies"][0]["reference_schema"] = "public"
		c["dependencies"][0]["reference"] = "public.synthetic_owned_function()"
		d := c["external_objects"][0]["definition"].(c18CatalogRow)
		d["name"] = "gen_random_uuid"
		d["schema"] = "pg_catalog"
		d["identity_arguments"] = ""
		d["return_type"] = "uuid"
		d["cost"] = json.Number("1")
		d["volatility"] = "v"
		schema := "pg_catalog"
		ref := &c18CatalogExternalReference{Kind: "c18_pg16_external_provenance_reference_v2", PGMajor: 16, C1809ReceiptSHA256: strings.Repeat("1", 64), DistributionSHA256: strings.Repeat("6", 64), InitdbEvidenceSHA256: strings.Repeat("3", 64), Extensions: map[string]c18CatalogExtensionArtifact{}, Profile: c18CatalogBootstrapProfile{ID: c18CatalogRequiredProfile, FieldsProtocol: c18CatalogReferenceFieldsProtocol, RoutineKinds: []string{"a", "f", "p", "w"}, RequiredBootstrap: []c18CatalogObjectKey{{"pg_proc", "pg_catalog.gen_random_uuid()", &schema}}}}
		// Copy the full approved definition so a live mutation cannot mutate its oracle.
		raw, e := c18CatalogBytes(d)
		if e != nil {
			t.Fatal(e)
		}
		var approved c18CatalogRow
		if e = c18CatalogDecode(raw, &approved); e != nil {
			t.Fatal(e)
		}
		ref.Objects = []c18CatalogExternalObject{{Class: "pg_proc", Identity: "pg_catalog.gen_random_uuid()", Schema: &schema, OriginKind: "verified_initdb_bootstrap", OriginName: "postgresql", OriginVersion: "16", ArtifactSHA256: ref.DistributionSHA256, Definition: approved}}
		for _, name := range []string{"plpgsql", "pgcrypto", "pg_trgm"} {
			ref.Extensions[name] = c18CatalogExtensionArtifact{"synthetic", strings.Repeat("4", 64), strings.Repeat("5", 64)}
		}
		live := []c18CatalogRequiredObject{{class: "pg_proc", identity: "pg_catalog.gen_random_uuid()", schema: &schema, oid: 314159, reasons: []string{"bootstrap_routine:f"}}}
		if e = c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e != nil {
			t.Fatal("complete signed-profile grammar positive:", e)
		}
		if e = c18CatalogValidateState(c); e != nil {
			t.Fatal("complete catalog-shape positive:", e)
		}
		if _, e = c18CatalogBuildRequired(ref, live, nil, nil); e != nil {
			t.Fatal("zero-edge required inventory positive:", e)
		}
		if _, e = c18CatalogApproveExternal(ref, live[0].class, live[0].identity, live[0].schema, d, "", ""); e != nil {
			t.Fatal("full live definition positive:", e)
		}
		return c, ref, live
	}
	for _, p := range []struct {
		name, key string
		value     any
	}{{"gen_random_uuid_cost", "cost", json.Number("2")}, {"gen_random_uuid_volatility", "volatility", "s"}} {
		t.Run(p.name, func(t *testing.T) {
			c, ref, live := fixture()
			d := c["external_objects"][0]["definition"].(c18CatalogRow)
			d[p.key] = p.value
			if e := c18CatalogValidateState(c); e != nil {
				t.Fatal("mutation must still pass full shape before semantic comparison:", e)
			}
			required, e := c18CatalogBuildRequired(ref, live, nil, nil)
			if e != nil || len(required) != 1 || required[0].oid != 314159 {
				t.Fatal("stable-ID independent required inventory changed")
			}
			if _, e = c18CatalogApproveExternal(ref, required[0].class, required[0].identity, required[0].schema, d, "", ""); e == nil || e.Error() != "external semantic definition drift" {
				t.Fatalf("zero-edge builtin semantic gate: %v", e)
			}
		})
	}
	t.Run("missing_live_builtin", func(t *testing.T) {
		_, ref, _ := fixture()
		if _, e := c18CatalogBuildRequired(ref, nil, nil, nil); e == nil || e.Error() != "required system definition missing" {
			t.Fatalf("missing live builtin gate: %v", e)
		}
	})
	t.Run("missing_approved_builtin", func(t *testing.T) {
		_, ref, _ := fixture()
		ref.Objects = nil
		if e := c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e == nil || e.Error() != "approved required definition missing" {
			t.Fatalf("missing approved builtin gate: %v", e)
		}
	})
	t.Run("profile_omits_builtin", func(t *testing.T) {
		_, ref, _ := fixture()
		ref.Profile.RequiredBootstrap = nil
		if e := c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e == nil || e.Error() != "required bootstrap builtin missing: pg_catalog.gen_random_uuid()" {
			t.Fatalf("profile omission gate: %v", e)
		}
	})
	t.Run("unknown_profile", func(t *testing.T) {
		_, ref, _ := fixture()
		ref.Profile.ID = "schema_or_oid_guess"
		if e := c18CatalogValidateExternalReference(ref, ref.C1809ReceiptSHA256); e == nil || e.Error() != "unknown required bootstrap profile" {
			t.Fatalf("unknown profile gate: %v", e)
		}
	})
	t.Run("coverage_not_dependency_edges", func(t *testing.T) {
		c, _, _ := fixture()
		c["external_objects"] = []c18CatalogRow{}
		if e := c18CatalogValidateState(c); e == nil || e.Error() != "required system definition missing" {
			t.Fatalf("independent coverage gate: %v", e)
		}
	})
	t.Run("required_set_cannot_drop_builtin", func(t *testing.T) {
		c, _, _ := fixture()
		c["required_external_objects"] = []c18CatalogRow{}
		c["external_objects"] = []c18CatalogRow{}
		if e := c18CatalogValidateState(c); e == nil || e.Error() != "required bootstrap builtin missing: pg_catalog.gen_random_uuid()" {
			t.Fatalf("required-set omission gate: %v", e)
		}
	})
}
