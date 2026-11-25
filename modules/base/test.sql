-- Semantics of the base module. Run from the repository root with psql on a migrated database with an empty domain:
-- the queries of gen/sqlc.json are prepared under their lowercase names; everything is rolled back.
\set ON_ERROR_STOP on
\o /dev/null
\set manifest `cat gen/sqlc.json`
begin;
select format('prepare %s as %s', lower(q->>'name'), q->>'text') from jsonb_array_elements(:'manifest'::jsonb -> 'queries') q \gexec

create function pg_temp.eq(label text, got anycompatible, want anycompatible) returns void language plpgsql as $$
begin
  if got is distinct from want then raise exception '%: got %, want %', label, got, want; end if;
end $$;
create function pg_temp.uid(s text) returns bigint language sql as $$ select id from "user" where subject = s $$;

-- Tree: boss > lead > dev, boss > solo; others is an unrelated human; bot is an agent.
insert into "user" (subject, name) values ('boss', 'Boss'), ('lead', 'Lead'), ('dev', 'Dev'), ('solo', 'Solo'), ('other', 'Other'), ('root', 'Root');
insert into "user" (kind, subject, name) values ('agent', 'bot', 'Bot');
update "user" set manager_id = pg_temp.uid('boss') where subject in ('lead', 'solo');
update "user" set manager_id = pg_temp.uid('lead') where subject = 'dev';
insert into user_group (name, admin) values ('g-admin', true), ('g-direct', false), ('g-tree', false), ('g-all', false);
insert into user_group_fn (user_group_id, fn)
select g.id, v.fn::fn from user_group g join (values ('g-direct', 'scope_direct'), ('g-direct', 'activity'), ('g-tree', 'scope_tree'), ('g-all', 'scope_all')) v(g, fn) on v.g = g.name;
insert into user_group_member (user_group_id, user_id)
select g.id, pg_temp.uid(v.s) from user_group g join (values ('g-admin', 'root'), ('g-direct', 'boss'), ('g-tree', 'lead'), ('g-all', 'other')) v(g, s) on v.g = g.name;

create temp table pr as execute principal('{root,boss,lead,other,dev}');

-- The admin has every function, scope_all included: its team is every other human.
select pg_temp.eq('admin functions', (select functions from pr where subject = 'root'), enum_range(null::fn));
select pg_temp.eq('admin self', (select reach -> 'self' from pr where subject = 'root'), jsonb_build_array(pg_temp.uid('root')));
select pg_temp.eq('admin team', (select reach -> 'team' from pr where subject = 'root'),
    (select jsonb_agg(id order by id) from "user" where kind = 'human' and subject <> 'root'));
select pg_temp.eq('admin kind/lang', (select kind || '/' || lang from pr where subject = 'root'), 'human/fr');

-- scope_direct: direct reports only; scope_tree: the whole subtree; scope_all: every other human, no agent.
select pg_temp.eq('direct team', (select reach -> 'team' from pr where subject = 'boss'), jsonb_build_array(pg_temp.uid('lead'), pg_temp.uid('solo')));
select pg_temp.eq('direct functions', (select functions from pr where subject = 'boss'), '{activity,scope_direct}'::fn[]);
select pg_temp.eq('tree team', (select reach -> 'team' from pr where subject = 'lead'), jsonb_build_array(pg_temp.uid('dev')));
select pg_temp.eq('all team', (select reach -> 'team' from pr where subject = 'other'),
    (select jsonb_agg(id order by id) from "user" where kind = 'human' and subject <> 'other'));
select pg_temp.eq('user = self + team', (select reach -> 'user' from pr where subject = 'boss'),
    jsonb_build_array(pg_temp.uid('boss'), pg_temp.uid('lead'), pg_temp.uid('solo')));
select pg_temp.eq('no function, no team', (select (reach -> 'team') || to_jsonb(functions) from pr where subject = 'dev'), '[]'::jsonb);

-- A deeper subtree is followed.
update "user" set manager_id = pg_temp.uid('dev') where subject = 'solo';
create temp table pr2 as execute principal('{lead}');
select pg_temp.eq('tree depth', (select reach -> 'team' from pr2), jsonb_build_array(pg_temp.uid('dev'), pg_temp.uid('solo')));

-- Archived principals are absent.
update "user" set archived = true where subject = 'other';
create temp table pr3 as execute principal('{other}');
select pg_temp.eq('archived', (select count(*) from pr3), 0::bigint);

\echo base: ok
rollback;
