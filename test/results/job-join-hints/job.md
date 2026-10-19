# LQO advisor report

- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2), database imdb (primary, allowed with --allow-primary)
- **Workload:** job.sql (sql): 113 statements, 94 shapes
- **Settings:** standard hints (13), 3 timed runs after a warm-up, up to 10 samples per shape, a hint must save 20%; row-count hints with pg_hint_plan
- **Estimate:** one pass of the tested statements takes 133,103 ms now, 69,825 ms with these hints (−48%)

## Faster with a hint (43 of 94 tested)

### `dd1b70c1` 346 ms → 203 ms (−41%)

```sql
SELECT MIN(t.title) AS movie_title
FROM company_name AS cn,
     keyword AS k,
     movie_companies AS mc,
     movie_keyword AS mk,
     title AS t
WHERE cn.country_code ='[de]'
  AND k.keyword ='character-name-in-title'
  AND cn.id = mc.company_id
  AND mc.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND mc.movie_id = mk.movie_id
```

- **Hint:** row counts, 4 calls
- **Plan:** Nested Loop ×2, Index Scan on movie_companies, Index Scan on company_name → Hash Join ×2, Seq Scan on movie_companies, Seq Scan on company_name

```sql
/*+ Rows(k mk *1200) Rows(k mc mk *800) Rows(cn k mc mk *980) Rows(cn k mc mk t *980) */ -- at the start of the query, with pg_hint_plan
```

### `718b520c` 445 ms → 127 ms (−72%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(miidx.info) AS rating,
       MIN(t.title) AS movie_about_winning
FROM company_name AS cn,
     company_type AS ct,
     info_type AS it,
     info_type AS it2,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS miidx,
     title AS t
WHERE cn.country_code ='[us]'
  AND ct.kind ='production companies'
  AND it.info ='rating'
  AND it2.info ='release dates'
  AND kt.kind ='movie'
  AND t.title != ''
  AND (t.title LIKE '%Champion%'
       OR t.title LIKE '%Loser%')
  AND mi.movie_id = t.id
  AND it2.id = mi.info_type_id
  AND kt.id = t.kind_id
  AND mc.movie_id = t.id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
  AND miidx.movie_id = t.id
  AND it.id = miidx.info_type_id
  AND mi.movie_id = miidx.movie_id
  AND mi.movie_id = mc.movie_id
  AND miidx.movie_id = mc.movie_id
```

- **Hint:** row counts, 2 calls
- **Plan:** Nested Loop, Seq Scan on movie_info_idx, Index Scan on title, Memoize, Index Scan on kind_type, Index Scan on company_type → Hash Join, Seq Scan on kind_type, Bitmap Heap Scan on title, Index Scan on movie_info_idx, Seq Scan on company_type

```sql
/*+ Rows(it miidx *30) Rows(it miidx t *120) Rows(it kt miidx t *30) Rows(it kt mc miidx t *100) Rows(ct it kt mc miidx t *39) Rows(cn ct it kt mc miidx t *20) Rows(cn ct it kt mc mi miidx t *350) Rows(cn ct it it2 kt mc mi miidx t *120) */ -- at the start of the query, with pg_hint_plan
```

### `a801be56` 3,862 ms → 2,466 ms (−36%)

```sql
SELECT MIN(n.name) AS member_in_charnamed_movie,
       MIN(n.name) AS a1
FROM cast_info AS ci,
     company_name AS cn,
     keyword AS k,
     movie_companies AS mc,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE k.keyword ='character-name-in-title'
  AND n.name LIKE 'Z%'
  AND n.id = ci.person_id
  AND ci.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.movie_id = mc.movie_id
  AND ci.movie_id = mk.movie_id
  AND mc.movie_id = mk.movie_id
```

- **Hint:** no nested loop, 2 calls
- **Plan:** Nested Loop ×6, Bitmap Heap Scan on movie_keyword, Index Scan on cast_info, Index Scan on name, Index Only Scan on title, Index Scan on movie_companies, Index Only Scan on company_name → Hash Join ×6, Seq Scan on company_name, Seq Scan on movie_companies, Seq Scan on title, Seq Scan on cast_info, Seq Scan on name, Seq Scan on movie_keyword

```sql
SET LOCAL enable_nestloop = off;
```

### `73f27e86` 4,686 ms → 1,726 ms (−63%)

```sql
SELECT MIN(n.name) AS member_in_charnamed_movie
FROM cast_info AS ci,
     company_name AS cn,
     keyword AS k,
     movie_companies AS mc,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE k.keyword ='character-name-in-title'
  AND n.name LIKE '%Bert%'
  AND n.id = ci.person_id
  AND ci.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.movie_id = mc.movie_id
  AND ci.movie_id = mk.movie_id
  AND mc.movie_id = mk.movie_id
```

- **Hint:** row counts, 2 calls
- **Plan:** Nested Loop, Index Scan on name → Hash Join, Seq Scan on name

```sql
/*+ Rows(k mk *1200) Rows(ci k mk *860) Rows(ci k mk n *2000) Rows(ci k mc mk n *12000) Rows(ci cn k mc mk n *12000) Rows(ci cn k mc mk n t *12000) */ -- at the start of the query, with pg_hint_plan
```

### `6d1d5dbb` 262 ms → 187 ms (−29%)

```sql
SELECT MIN(kt.kind) AS movie_kind,
       MIN(t.title) AS complete_us_internet_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     keyword AS k,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     title AS t
WHERE cct1.kind = 'complete+verified'
  AND cn.country_code = '[us]'
  AND it1.info = 'release dates'
  AND kt.kind IN ('movie')
  AND mi.note LIKE '%internet%'
  AND mi.info IS NOT NULL
  AND (mi.info LIKE 'USA:% 199%'
       OR mi.info LIKE 'USA:% 200%')
  AND t.production_year > 2000
  AND kt.id = t.kind_id
  AND t.id = mi.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND t.id = cc.movie_id
  AND mk.movie_id = mi.movie_id
  AND mk.movie_id = mc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND k.id = mk.keyword_id
  AND it1.id = mi.info_type_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
  AND cct1.id = cc.status_id
```

- **Hint:** no hash join, 2 calls
- **Plan:** Hash Join ×3, Seq Scan on title, Memoize, Index Scan on info_type, Index Only Scan on company_type → Nested Loop ×3, Seq Scan on info_type, Index Scan on title, Seq Scan on company_type

```sql
SET LOCAL enable_hashjoin = off;
```

### `0cf295d8` 75.1 ms → 4.99 ms (−93%)

```sql
SELECT MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_title,
       MIN(t.production_year) AS movie_year
FROM company_type AS ct,
     info_type AS it,
     movie_companies AS mc,
     movie_info_idx AS mi_idx,
     title AS t
WHERE ct.kind = 'production companies'
  AND it.info = 'top 250 rank'
  AND mc.note NOT LIKE '%(as Metro-Goldwyn-Mayer Pictures)%'
  AND (mc.note LIKE '%(co-production)%'
       OR mc.note LIKE '%(presents)%')
  AND ct.id = mc.company_type_id
  AND t.id = mc.movie_id
  AND t.id = mi_idx.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND it.id = mi_idx.info_type_id
```

- **Hint:** no hash join, 1 call
- **Plan:** Hash Join ×2, Seq Scan on movie_info_idx → Nested Loop ×2, Bitmap Heap Scan on movie_info_idx

```sql
SET LOCAL enable_hashjoin = off;
```

### `078dfa3f` 69.1 ms → 0.69 ms (−99%)

```sql
SELECT MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_title,
       MIN(t.production_year) AS movie_year
FROM company_type AS ct,
     info_type AS it,
     movie_companies AS mc,
     movie_info_idx AS mi_idx,
     title AS t
WHERE ct.kind = 'production companies'
  AND it.info = 'bottom 10 rank'
  AND mc.note NOT LIKE '%(as Metro-Goldwyn-Mayer Pictures)%'
  AND t.production_year BETWEEN 2005 AND 2010
  AND ct.id = mc.company_type_id
  AND t.id = mc.movie_id
  AND t.id = mi_idx.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND it.id = mi_idx.info_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join, Seq Scan on movie_info_idx → Nested Loop, Bitmap Heap Scan on movie_info_idx

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `ae99dc85` 73.8 ms → 3.80 ms (−95%)

```sql
SELECT MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_title,
       MIN(t.production_year) AS movie_year
FROM company_type AS ct,
     info_type AS it,
     movie_companies AS mc,
     movie_info_idx AS mi_idx,
     title AS t
WHERE ct.kind = 'production companies'
  AND it.info = 'top 250 rank'
  AND mc.note NOT LIKE '%(as Metro-Goldwyn-Mayer Pictures)%'
  AND (mc.note LIKE '%(co-production)%')
  AND t.production_year >2010
  AND ct.id = mc.company_type_id
  AND t.id = mc.movie_id
  AND t.id = mi_idx.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND it.id = mi_idx.info_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join, Seq Scan on movie_info_idx → Nested Loop, Bitmap Heap Scan on movie_info_idx

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `450fe5f6` 68.7 ms → 0.72 ms (−99%)

```sql
SELECT MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_title,
       MIN(t.production_year) AS movie_year
FROM company_type AS ct,
     info_type AS it,
     movie_companies AS mc,
     movie_info_idx AS mi_idx,
     title AS t
WHERE ct.kind = 'production companies'
  AND it.info = 'bottom 10 rank'
  AND mc.note NOT LIKE '%(as Metro-Goldwyn-Mayer Pictures)%'
  AND t.production_year >2000
  AND ct.id = mc.company_type_id
  AND t.id = mc.movie_id
  AND t.id = mi_idx.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND it.id = mi_idx.info_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join, Seq Scan on movie_info_idx → Nested Loop, Bitmap Heap Scan on movie_info_idx

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `cc66d5be` 475 ms → 69.3 ms (−85%)

```sql
SELECT MIN(n.name) AS of_person,
       MIN(t.title) AS biography_movie
FROM aka_name AS an,
     cast_info AS ci,
     info_type AS it,
     link_type AS lt,
     movie_link AS ml,
     name AS n,
     person_info AS pi,
     title AS t
WHERE an.name LIKE '%a%'
  AND it.info ='mini biography'
  AND lt.link ='features'
  AND n.name_pcode_cf BETWEEN 'A' AND 'F'
  AND (n.gender='m'
       OR (n.gender = 'f'
           AND n.name LIKE 'B%'))
  AND pi.note ='Volker Boehm'
  AND t.production_year BETWEEN 1980 AND 1995
  AND n.id = an.person_id
  AND n.id = pi.person_id
  AND ci.person_id = n.id
  AND t.id = ci.movie_id
  AND ml.linked_movie_id = t.id
  AND lt.id = ml.link_type_id
  AND it.id = pi.info_type_id
  AND pi.person_id = an.person_id
  AND pi.person_id = ci.person_id
  AND an.person_id = ci.person_id
  AND ci.movie_id = ml.linked_movie_id
```

- **Hint:** row counts, second pass, 1 call
- **Plan:** Nested Loop, Index Scan on person_info, Index Scan on info_type → Hash Join, Seq Scan on info_type, Bitmap Heap Scan on person_info, Memoize

```sql
/*+ Rows(it pi *64) Rows(lt ml *3.1) Rows(it n pi *17) Rows(lt ml t *6.5) Rows(ci it n pi *2700) Rows(ci lt ml t *13) Rows(an ci it n pi *3400) Rows(ci lt ml n t *19) Rows(an ci it ml n pi *510) Rows(ci lt ml n pi t *14) Rows(an ci it ml n pi t *110) Rows(an ci lt ml n pi t *32) Rows(an ci it lt ml n pi t *32) */ -- at the start of the query, with pg_hint_plan
```

### `46b42be8` 132 ms → 63.7 ms (−52%)

```sql
SELECT MIN(n.name) AS of_person,
       MIN(t.title) AS biography_movie
FROM aka_name AS an,
     cast_info AS ci,
     info_type AS it,
     link_type AS lt,
     movie_link AS ml,
     name AS n,
     person_info AS pi,
     title AS t
WHERE an.name LIKE '%a%'
  AND it.info ='mini biography'
  AND lt.link ='features'
  AND n.name_pcode_cf LIKE 'D%'
  AND n.gender='m'
  AND pi.note ='Volker Boehm'
  AND t.production_year BETWEEN 1980 AND 1984
  AND n.id = an.person_id
  AND n.id = pi.person_id
  AND ci.person_id = n.id
  AND t.id = ci.movie_id
  AND ml.linked_movie_id = t.id
  AND lt.id = ml.link_type_id
  AND it.id = pi.info_type_id
  AND pi.person_id = an.person_id
  AND pi.person_id = ci.person_id
  AND an.person_id = ci.person_id
  AND ci.movie_id = ml.linked_movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** Seq Scan on link_type, Bitmap Heap Scan on movie_link, Index Scan on person_info, Index Scan on info_type → Seq Scan on info_type, Bitmap Heap Scan on person_info, Index Scan on movie_link, Index Scan on link_type

```sql
/*+ Rows(lt ml *3.1) Rows(lt ml t *8.9) Rows(ci lt ml t *19) Rows(ci lt ml n t *27) Rows(ci lt ml n pi t *8) Rows(an ci lt ml n pi t *16) Rows(an ci it lt ml n pi t *16) */ -- at the start of the query, with pg_hint_plan
```

### `b8a77e31` 2,134 ms → 1,123 ms (−47%)

```sql
SELECT MIN(n.name) AS cast_member_name,
       MIN(pi.info) AS cast_member_info
FROM aka_name AS an,
     cast_info AS ci,
     info_type AS it,
     link_type AS lt,
     movie_link AS ml,
     name AS n,
     person_info AS pi,
     title AS t
WHERE an.name IS NOT NULL
  AND (an.name LIKE '%a%'
       OR an.name LIKE 'A%')
  AND it.info ='mini biography'
  AND lt.link IN ('references',
                  'referenced in',
                  'features',
                  'featured in')
  AND n.name_pcode_cf BETWEEN 'A' AND 'F'
  AND (n.gender='m'
       OR (n.gender = 'f'
           AND n.name LIKE 'A%'))
  AND pi.note IS NOT NULL
  AND t.production_year BETWEEN 1980 AND 2010
  AND n.id = an.person_id
  AND n.id = pi.person_id
  AND ci.person_id = n.id
  AND t.id = ci.movie_id
  AND ml.linked_movie_id = t.id
  AND lt.id = ml.link_type_id
  AND it.id = pi.info_type_id
  AND pi.person_id = an.person_id
  AND pi.person_id = ci.person_id
  AND an.person_id = ci.person_id
  AND ci.movie_id = ml.linked_movie_id
```

- **Hint:** no index scan, 1 call
- **Plan:** Nested Loop, Index Scan on aka_name, Index Scan on name, Index Scan on cast_info, Index Scan on movie_link, Index Scan on title → Hash Join, Bitmap Heap Scan on name, Bitmap Heap Scan on aka_name, Bitmap Heap Scan on cast_info, Seq Scan on movie_link, Bitmap Heap Scan on title

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

### `70b0406f` 292 ms → 208 ms (−29%)

```sql
SELECT MIN(chn.name) AS uncredited_voiced_character,
       MIN(t.title) AS russian_movie
FROM char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     company_type AS ct,
     movie_companies AS mc,
     role_type AS rt,
     title AS t
WHERE ci.note LIKE '%(voice)%'
  AND ci.note LIKE '%(uncredited)%'
  AND cn.country_code = '[ru]'
  AND rt.role = 'actor'
  AND t.production_year > 2005
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND ci.movie_id = mc.movie_id
  AND chn.id = ci.person_role_id
  AND rt.id = ci.role_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no hash join, 1 call
- **Plan:** Hash Join ×2, Seq Scan on movie_companies, Seq Scan on role_type → Nested Loop ×2, Index Scan on movie_companies, Memoize ×2, Index Scan on role_type

```sql
SET LOCAL enable_hashjoin = off;
```

### `6eab06ef` 212 ms → 98.6 ms (−53%)

```sql
SELECT MIN(chn.name) AS character,
       MIN(t.title) AS russian_mov_with_actor_producer
FROM char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     company_type AS ct,
     movie_companies AS mc,
     role_type AS rt,
     title AS t
WHERE ci.note LIKE '%(producer)%'
  AND cn.country_code = '[ru]'
  AND rt.role = 'actor'
  AND t.production_year > 2010
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND ci.movie_id = mc.movie_id
  AND chn.id = ci.person_role_id
  AND rt.id = ci.role_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no hash join, 1 call
- **Plan:** Hash Join ×3, Seq Scan on movie_companies, Seq Scan on role_type, Seq Scan on company_type → Nested Loop ×3, Index Scan on movie_companies, Memoize ×2, Index Scan on role_type, Index Only Scan on company_type

```sql
SET LOCAL enable_hashjoin = off;
```

### `534b68fc` 5,138 ms → 2,388 ms (−54%)

```sql
SELECT MIN(chn.name) AS character,
       MIN(t.title) AS movie_with_american_producer
FROM char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     company_type AS ct,
     movie_companies AS mc,
     role_type AS rt,
     title AS t
WHERE ci.note LIKE '%(producer)%'
  AND cn.country_code = '[us]'
  AND t.production_year > 1990
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND ci.movie_id = mc.movie_id
  AND chn.id = ci.person_role_id
  AND rt.id = ci.role_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no nested loop, 1 call
- **Plan:** Nested Loop ×2, Index Scan on cast_info, Memoize, Index Scan on char_name → Hash Join ×2, Seq Scan on cast_info, Seq Scan on char_name

```sql
SET LOCAL enable_nestloop = off;
```

### `c3eba7e4` 46.7 ms → 25.7 ms (−45%)

```sql
SELECT MIN(cn.name) AS from_company,
       MIN(lt.link) AS movie_link_type,
       MIN(t.title) AS non_polish_sequel_movie
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[pl]'
  AND (cn.name LIKE '%Film%'
       OR cn.name LIKE '%Warner%')
  AND ct.kind ='production companies'
  AND k.keyword ='sequel'
  AND lt.link LIKE '%follow%'
  AND mc.note IS NULL
  AND t.production_year BETWEEN 1950 AND 2000
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** same joins and scans

```sql
/*+ Rows(k lt mk ml *200) Rows(k lt mk ml t *150) Rows(k lt mc mk ml t *710) Rows(cn k lt mc mk ml t *330) Rows(cn ct k lt mc mk ml t *310) */ -- at the start of the query, with pg_hint_plan
```

### `850e1a80` 185 ms → 105 ms (−43%)

```sql
SELECT MIN(cn.name) AS from_company,
       MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_based_on_book
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[pl]'
  AND (cn.name LIKE '20th Century Fox%'
       OR cn.name LIKE 'Twentieth Century Fox%')
  AND ct.kind != 'production companies'
  AND ct.kind IS NOT NULL
  AND k.keyword IN ('sequel',
                    'revenge',
                    'based-on-novel')
  AND mc.note IS NOT NULL
  AND t.production_year > 1950
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Memoize, Index Scan on company_type, Index Only Scan on link_type → Seq Scan on company_type, Seq Scan on link_type

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `8461866c` 169 ms → 92.8 ms (−45%)

```sql
SELECT MIN(cn.name) AS from_company,
       MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_based_on_book
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[pl]'
  AND ct.kind != 'production companies'
  AND ct.kind IS NOT NULL
  AND k.keyword IN ('sequel',
                    'revenge',
                    'based-on-novel')
  AND mc.note IS NOT NULL
  AND t.production_year > 1950
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join ×2 → Nested Loop ×2

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `729ccb90` 75.4 ms → 2.77 ms (−96%)

```sql
SELECT MIN(mi.info) AS budget,
       MIN(t.title) AS unsuccsessful_movie
FROM company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     info_type AS it2,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     title AS t
WHERE cn.country_code ='[us]'
  AND ct.kind IS NOT NULL
  AND (ct.kind ='production companies'
       OR ct.kind = 'distributors')
  AND it1.info ='budget'
  AND it2.info ='bottom 10 rank'
  AND t.production_year >2000
  AND (t.title LIKE 'Birdemic%'
       OR t.title LIKE '%Movie%')
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND mi.info_type_id = it1.id
  AND mi_idx.info_type_id = it2.id
  AND t.id = mc.movie_id
  AND ct.id = mc.company_type_id
  AND cn.id = mc.company_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND mi.movie_id = mi_idx.movie_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join, Seq Scan on movie_info_idx → Nested Loop, Bitmap Heap Scan on movie_info_idx

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `91a61e77` 1,024 ms → 484 ms (−53%)

```sql
SELECT MIN(mi.info) AS release_date,
       MIN(miidx.info) AS rating,
       MIN(t.title) AS german_movie
FROM company_name AS cn,
     company_type AS ct,
     info_type AS it,
     info_type AS it2,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS miidx,
     title AS t
WHERE cn.country_code ='[de]'
  AND ct.kind ='production companies'
  AND it.info ='rating'
  AND it2.info ='release dates'
  AND kt.kind ='movie'
  AND mi.movie_id = t.id
  AND it2.id = mi.info_type_id
  AND kt.id = t.kind_id
  AND mc.movie_id = t.id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
  AND miidx.movie_id = t.id
  AND it.id = miidx.info_type_id
  AND mi.movie_id = miidx.movie_id
  AND mi.movie_id = mc.movie_id
  AND miidx.movie_id = mc.movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** Nested Loop, Seq Scan on movie_info_idx, Index Scan on movie_companies, Index Scan on company_name → Hash Join, Seq Scan on movie_companies, Seq Scan on company_name, Index Scan on movie_info_idx

```sql
/*+ Rows(it miidx *30) Rows(it miidx t *30) Rows(it kt miidx t *96) Rows(it kt mc miidx t *65) Rows(ct it kt mc miidx t *100) Rows(cn ct it kt mc miidx t *130) Rows(cn ct it kt mc mi miidx t *130) Rows(cn ct it it2 kt mc mi miidx t *3100) */ -- at the start of the query, with pg_hint_plan
```

### `5798935c` 475 ms → 370 ms (−22%)

```sql
SELECT MIN(at.title) AS aka_title,
       MIN(t.title) AS internet_movie_title
FROM aka_title AS at,
     company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     title AS t
WHERE cn.country_code = '[us]'
  AND it1.info = 'release dates'
  AND mi.note LIKE '%internet%'
  AND t.production_year > 1990
  AND t.id = at.movie_id
  AND t.id = mi.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND mk.movie_id = mi.movie_id
  AND mk.movie_id = mc.movie_id
  AND mk.movie_id = at.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi.movie_id = at.movie_id
  AND mc.movie_id = at.movie_id
  AND k.id = mk.keyword_id
  AND it1.id = mi.info_type_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no index scan, 1 call
- **Plan:** Nested Loop, Index Scan on title, Index Scan on movie_companies, Index Scan on company_name, Index Scan on movie_keyword, Index Only Scan on keyword, Index Scan on aka_title → Hash Join, Bitmap Heap Scan on aka_title, Bitmap Heap Scan on title, Bitmap Heap Scan on movie_companies, Bitmap Heap Scan on company_name, Bitmap Heap Scan on movie_keyword, Seq Scan on keyword

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

### `8f30d4e5` 11,721 ms → 3,446 ms (−71%)

```sql
SELECT MIN(an.name) AS cool_actor_pseudonym,
       MIN(t.title) AS series_named_after_char
FROM aka_name AS an,
     cast_info AS ci,
     company_name AS cn,
     keyword AS k,
     movie_companies AS mc,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cn.country_code ='[us]'
  AND k.keyword ='character-name-in-title'
  AND an.person_id = n.id
  AND n.id = ci.person_id
  AND ci.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_id = cn.id
  AND an.person_id = ci.person_id
  AND ci.movie_id = mc.movie_id
  AND ci.movie_id = mk.movie_id
  AND mc.movie_id = mk.movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** Nested Loop ×4, Index Scan on title, Index Scan on movie_companies, Index Scan on company_name, Index Scan on aka_name → Hash Join ×4, Seq Scan on movie_companies, Seq Scan on company_name, Memoize, Seq Scan on aka_name, Seq Scan on title

```sql
/*+ Rows(k mk *1200) Rows(k mk t *1200) Rows(k mc mk t *800) Rows(cn k mc mk t *1000) Rows(ci cn k mc mk t *2500) Rows(ci cn k mc mk n t *2500) Rows(an ci cn k mc mk n t *1400) */ -- at the start of the query, with pg_hint_plan
```

### `6d17eb01` 7,194 ms → 2,399 ms (−67%)

```sql
SELECT MIN(n.name) AS member_in_charnamed_american_movie,
       MIN(n.name) AS a1
FROM cast_info AS ci,
     company_name AS cn,
     keyword AS k,
     movie_companies AS mc,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cn.country_code ='[us]'
  AND k.keyword ='character-name-in-title'
  AND n.name LIKE 'B%'
  AND n.id = ci.person_id
  AND ci.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.movie_id = mc.movie_id
  AND ci.movie_id = mk.movie_id
  AND mc.movie_id = mk.movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** Nested Loop ×2, Index Scan on movie_companies, Index Scan on company_name → Hash Join ×2, Seq Scan on movie_companies, Seq Scan on company_name, Memoize

```sql
/*+ Rows(k mk *1200) Rows(k mk t *1200) Rows(k mc mk t *800) Rows(cn k mc mk t *1000) Rows(ci cn k mc mk t *2500) Rows(ci cn k mc mk n t *2800) */ -- at the start of the query, with pg_hint_plan
```

### `85cf65fe` 7,203 ms → 2,871 ms (−60%)

```sql
SELECT MIN(n.name) AS member_in_charnamed_movie
FROM cast_info AS ci,
     company_name AS cn,
     keyword AS k,
     movie_companies AS mc,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cn.country_code ='[us]'
  AND k.keyword ='character-name-in-title'
  AND n.id = ci.person_id
  AND ci.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.movie_id = mc.movie_id
  AND ci.movie_id = mk.movie_id
  AND mc.movie_id = mk.movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** Nested Loop ×2, Index Scan on movie_companies, Index Scan on company_name → Hash Join ×2, Seq Scan on movie_companies, Seq Scan on company_name, Memoize ×2

```sql
/*+ Rows(k mk *1200) Rows(k mk t *1200) Rows(k mc mk t *800) Rows(cn k mc mk t *1000) Rows(ci cn k mc mk t *2500) Rows(ci cn k mc mk n t *2500) */ -- at the start of the query, with pg_hint_plan
```

### `2656274c` 2,338 ms → 381 ms (−84%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(t.title) AS movie_title
FROM cast_info AS ci,
     info_type AS it1,
     info_type AS it2,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     name AS n,
     title AS t
WHERE ci.note IN ('(producer)',
                  '(executive producer)')
  AND it1.info = 'budget'
  AND it2.info = 'votes'
  AND n.gender = 'm'
  AND n.name LIKE '%Tim%'
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
```

- **Hint:** row counts, 1 call
- **Plan:** Hash Join, Seq Scan on movie_info_idx, Seq Scan on info_type, Index Scan on name → Nested Loop, Seq Scan on name, Index Scan on movie_info_idx, Memoize, Index Scan on info_type

```sql
/*+ Rows(it2 mi_idx *30) Rows(it2 mi mi_idx *14) Rows(it1 it2 mi mi_idx *11) Rows(ci it1 it2 mi mi_idx *10) Rows(ci it1 it2 mi mi_idx n *140) Rows(ci it1 it2 mi mi_idx n t *410) */ -- at the start of the query, with pg_hint_plan
```

### `8cf8918f` 8,345 ms → 3,017 ms (−64%)

```sql
SELECT MIN(n.name) AS voicing_actress,
       MIN(t.title) AS jap_engl_voiced_movie
FROM aka_name AS an,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     info_type AS it,
     movie_companies AS mc,
     movie_info AS mi,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note IN ('(voice)',
                  '(voice: Japanese version)',
                  '(voice) (uncredited)',
                  '(voice: English version)')
  AND cn.country_code ='[us]'
  AND it.info = 'release dates'
  AND n.gender ='f'
  AND rt.role ='actress'
  AND t.production_year > 2000
  AND t.id = mi.movie_id
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND mc.movie_id = ci.movie_id
  AND mc.movie_id = mi.movie_id
  AND mi.movie_id = ci.movie_id
  AND cn.id = mc.company_id
  AND it.id = mi.info_type_id
  AND n.id = ci.person_id
  AND rt.id = ci.role_id
  AND n.id = an.person_id
  AND ci.person_id = an.person_id
  AND chn.id = ci.person_role_id
```

- **Hint:** no memoize, 1 call
- **Plan:** Nested Loop ×3, Memoize, Index Scan on movie_companies, Index Scan on company_name → Hash Join ×3, Seq Scan on movie_companies, Seq Scan on company_name

```sql
SET LOCAL enable_memoize = off;
```

### `0cc52afa` 51.3 ms → 31.4 ms (−39%)

```sql
SELECT MIN(cn.name) AS company_name,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS german_follow_up
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[pl]'
  AND (cn.name LIKE '%Film%'
       OR cn.name LIKE '%Warner%')
  AND ct.kind ='production companies'
  AND k.keyword ='sequel'
  AND lt.link LIKE '%follow%'
  AND mc.note IS NULL
  AND mi.info IN ('Germany',
                  'German')
  AND t.production_year BETWEEN 2000 AND 2010
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
```

- **Hint:** row counts, 1 call
- **Plan:** same joins and scans

```sql
/*+ Rows(k lt mk ml *200) Rows(k lt mi mk ml *120) Rows(k lt mc mi mk ml *750) Rows(cn k lt mc mi mk ml *330) Rows(cn ct k lt mc mi mk ml *310) Rows(cn ct k lt mc mi mk ml t *38) */ -- at the start of the query, with pg_hint_plan
```

### `39172b8c` 223 ms → 132 ms (−41%)

```sql
SELECT MIN(chn.name) AS voiced_char_name,
       MIN(n.name) AS voicing_actress_name,
       MIN(t.title) AS voiced_action_movie_jap_eng
FROM aka_name AS an,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     info_type AS it,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note IN ('(voice)',
                  '(voice: Japanese version)',
                  '(voice) (uncredited)',
                  '(voice: English version)')
  AND cn.country_code ='[us]'
  AND it.info = 'release dates'
  AND k.keyword IN ('hero',
                    'martial-arts',
                    'hand-to-hand-combat')
  AND mi.info IS NOT NULL
  AND (mi.info LIKE 'Japan:%201%'
       OR mi.info LIKE 'USA:%201%')
  AND n.gender ='f'
  AND n.name LIKE '%An%'
  AND rt.role ='actress'
  AND t.production_year > 2010
  AND t.id = mi.movie_id
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND mc.movie_id = ci.movie_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mk.movie_id
  AND mi.movie_id = ci.movie_id
  AND mi.movie_id = mk.movie_id
  AND ci.movie_id = mk.movie_id
  AND cn.id = mc.company_id
  AND it.id = mi.info_type_id
  AND n.id = ci.person_id
  AND rt.id = ci.role_id
  AND n.id = an.person_id
  AND ci.person_id = an.person_id
  AND chn.id = ci.person_role_id
  AND k.id = mk.keyword_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `6ef26656` 100 ms → 58.7 ms (−42%)

```sql
SELECT MIN(chn.name) AS voiced_char_name,
       MIN(n.name) AS voicing_actress_name,
       MIN(t.title) AS kung_fu_panda
FROM aka_name AS an,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     info_type AS it,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note IN ('(voice)',
                  '(voice: Japanese version)',
                  '(voice) (uncredited)',
                  '(voice: English version)')
  AND cn.country_code ='[us]'
  AND cn.name = 'DreamWorks Animation'
  AND it.info = 'release dates'
  AND k.keyword IN ('hero',
                    'martial-arts',
                    'hand-to-hand-combat',
                    'computer-animated-movie')
  AND mi.info IS NOT NULL
  AND (mi.info LIKE 'Japan:%201%'
       OR mi.info LIKE 'USA:%201%')
  AND n.gender ='f'
  AND n.name LIKE '%An%'
  AND rt.role ='actress'
  AND t.production_year > 2010
  AND t.title LIKE 'Kung Fu Panda%'
  AND t.id = mi.movie_id
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND mc.movie_id = ci.movie_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mk.movie_id
  AND mi.movie_id = ci.movie_id
  AND mi.movie_id = mk.movie_id
  AND ci.movie_id = mk.movie_id
  AND cn.id = mc.company_id
  AND it.id = mi.info_type_id
  AND n.id = ci.person_id
  AND rt.id = ci.role_id
  AND n.id = an.person_id
  AND ci.person_id = an.person_id
  AND chn.id = ci.person_role_id
  AND k.id = mk.keyword_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Index Scan on info_type, Index Scan on role_type → Seq Scan on role_type, Seq Scan on info_type

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `92588c28` 4,580 ms → 3,628 ms (−21%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS male_writer,
       MIN(t.title) AS violent_movie_title
FROM cast_info AS ci,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE ci.note IN ('(writer)',
                  '(head writer)',
                  '(written by)',
                  '(story)',
                  '(story editor)')
  AND it1.info = 'genres'
  AND it2.info = 'votes'
  AND k.keyword IN ('murder',
                    'violence',
                    'blood',
                    'gore',
                    'death',
                    'female-nudity',
                    'hospital')
  AND mi.info IN ('Horror',
                  'Action',
                  'Sci-Fi',
                  'Thriller',
                  'Crime',
                  'War')
  AND n.gender = 'm'
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
```

- **Hint:** no nested loop, 1 call
- **Plan:** Nested Loop ×8, Bitmap Heap Scan on movie_keyword, Index Scan on movie_info_idx, Index Scan on movie_info, Index Scan on cast_info, Index Scan on name, Index Scan on title → Hash Join ×8, Seq Scan on title, Seq Scan on name, Seq Scan on cast_info, Seq Scan on movie_info_idx, Seq Scan on movie_info, Seq Scan on movie_keyword

```sql
SET LOCAL enable_nestloop = off;
```

### `2066cfd6` 18,311 ms → 517 ms (−97%)

```sql
SELECT MIN(chn.name) AS character_name,
       MIN(mi_idx.info) AS rating,
       MIN(n.name) AS playing_actor,
       MIN(t.title) AS complete_hero_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     char_name AS chn,
     cast_info AS ci,
     info_type AS it2,
     keyword AS k,
     kind_type AS kt,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind LIKE '%complete%'
  AND chn.name IS NOT NULL
  AND (chn.name LIKE '%man%'
       OR chn.name LIKE '%Man%')
  AND it2.info = 'rating'
  AND k.keyword IN ('superhero',
                    'marvel-comics',
                    'based-on-comic',
                    'tv-special',
                    'fight',
                    'violence',
                    'magnet',
                    'web',
                    'claw',
                    'laser')
  AND kt.kind = 'movie'
  AND mi_idx.info > '7.0'
  AND t.production_year > 2000
  AND kt.id = t.kind_id
  AND t.id = mk.movie_id
  AND t.id = ci.movie_id
  AND t.id = cc.movie_id
  AND t.id = mi_idx.movie_id
  AND mk.movie_id = ci.movie_id
  AND mk.movie_id = cc.movie_id
  AND mk.movie_id = mi_idx.movie_id
  AND ci.movie_id = cc.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND cc.movie_id = mi_idx.movie_id
  AND chn.id = ci.person_role_id
  AND n.id = ci.person_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND it2.id = mi_idx.info_type_id
```

- **Hint:** no index scan, 1 call
- **Plan:** Hash Join ×2, Seq Scan on complete_cast, Index Scan on movie_info_idx, Index Scan on title, Index Scan on cast_info, Index Scan on name, Index Scan on movie_keyword, Index Scan on keyword, Index Scan on char_name → Nested Loop ×2, Seq Scan on keyword, Bitmap Heap Scan on movie_keyword, Bitmap Heap Scan on movie_info_idx, Bitmap Heap Scan on title, Bitmap Heap Scan on complete_cast, Bitmap Heap Scan on cast_info, Bitmap Heap Scan on char_name, Bitmap Heap Scan on name

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

### `188e45da` 71.7 ms → 54.1 ms (−25%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS complete_western_sequel
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind = 'complete'
  AND cn.country_code !='[pl]'
  AND (cn.name LIKE '%Film%'
       OR cn.name LIKE '%Warner%')
  AND ct.kind ='production companies'
  AND k.keyword ='sequel'
  AND lt.link LIKE '%follow%'
  AND mc.note IS NULL
  AND mi.info IN ('Sweden',
                  'Germany',
                  'Swedish',
                  'German')
  AND t.production_year BETWEEN 1950 AND 2000
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND t.id = cc.movie_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
  AND ml.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = cc.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×3, Merge Join, Seq Scan on comp_cast_type → Nested Loop ×4, Memoize, Index Scan on comp_cast_type

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `4a1dc513` 61.4 ms → 43.8 ms (−29%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS complete_western_sequel
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind = 'complete'
  AND cn.country_code !='[pl]'
  AND (cn.name LIKE '%Film%'
       OR cn.name LIKE '%Warner%')
  AND ct.kind ='production companies'
  AND k.keyword ='sequel'
  AND lt.link LIKE '%follow%'
  AND mc.note IS NULL
  AND mi.info IN ('Sweden',
                  'Germany',
                  'Swedish',
                  'German')
  AND t.production_year = 1998
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND t.id = cc.movie_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
  AND ml.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = cc.movie_id
```

- **Hint:** no memoize, 1 call
- **Plan:** Nested Loop, Memoize, Index Scan on company_type → Hash Join, Seq Scan on company_type

```sql
SET LOCAL enable_memoize = off;
```

### `c862384f` 79.6 ms → 50.4 ms (−37%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS complete_western_sequel
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind LIKE 'complete%'
  AND cn.country_code !='[pl]'
  AND (cn.name LIKE '%Film%'
       OR cn.name LIKE '%Warner%')
  AND ct.kind ='production companies'
  AND k.keyword ='sequel'
  AND lt.link LIKE '%follow%'
  AND mc.note IS NULL
  AND mi.info IN ('Sweden',
                  'Norway',
                  'Germany',
                  'Denmark',
                  'Swedish',
                  'Denish',
                  'Norwegian',
                  'German',
                  'English')
  AND t.production_year BETWEEN 1950 AND 2010
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND t.id = cc.movie_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
  AND ml.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = cc.movie_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `b26e54ae` 820 ms → 294 ms (−64%)

```sql
SELECT MIN(cn.name) AS movie_company,
       MIN(mi_idx.info) AS rating,
       MIN(t.title) AS complete_euro_dark_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     title AS t
WHERE cct1.kind = 'crew'
  AND cct2.kind != 'complete+verified'
  AND cn.country_code != '[us]'
  AND it1.info = 'countries'
  AND it2.info = 'rating'
  AND k.keyword IN ('murder',
                    'murder-in-title',
                    'blood',
                    'violence')
  AND kt.kind IN ('movie',
                  'episode')
  AND mc.note NOT LIKE '%(USA)%'
  AND mc.note LIKE '%(200%)%'
  AND mi.info IN ('Sweden',
                  'Norway',
                  'Germany',
                  'Denmark',
                  'Swedish',
                  'Danish',
                  'Norwegian',
                  'German',
                  'USA',
                  'American')
  AND mi_idx.info < '8.5'
  AND t.production_year > 2000
  AND kt.id = t.kind_id
  AND t.id = mi.movie_id
  AND t.id = mk.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = mc.movie_id
  AND t.id = cc.movie_id
  AND mk.movie_id = mi.movie_id
  AND mk.movie_id = mi_idx.movie_id
  AND mk.movie_id = mc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi.movie_id = cc.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi_idx.movie_id = cc.movie_id
  AND k.id = mk.keyword_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND ct.id = mc.company_type_id
  AND cn.id = mc.company_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `e4943b34` 335 ms → 226 ms (−33%)

```sql
SELECT MIN(cn.name) AS movie_company,
       MIN(mi_idx.info) AS rating,
       MIN(t.title) AS complete_euro_dark_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     title AS t
WHERE cct1.kind = 'crew'
  AND cct2.kind != 'complete+verified'
  AND cn.country_code != '[us]'
  AND it1.info = 'countries'
  AND it2.info = 'rating'
  AND k.keyword IN ('murder',
                    'murder-in-title',
                    'blood',
                    'violence')
  AND kt.kind IN ('movie',
                  'episode')
  AND mc.note NOT LIKE '%(USA)%'
  AND mc.note LIKE '%(200%)%'
  AND mi.info IN ('Sweden',
                  'Germany',
                  'Swedish',
                  'German')
  AND mi_idx.info > '6.5'
  AND t.production_year > 2005
  AND kt.id = t.kind_id
  AND t.id = mi.movie_id
  AND t.id = mk.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = mc.movie_id
  AND t.id = cc.movie_id
  AND mk.movie_id = mi.movie_id
  AND mk.movie_id = mi_idx.movie_id
  AND mk.movie_id = mc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi.movie_id = cc.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi_idx.movie_id = cc.movie_id
  AND k.id = mk.keyword_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND ct.id = mc.company_type_id
  AND cn.id = mc.company_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no memoize, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL enable_memoize = off;
```

### `05ae6e9d` 95.2 ms → 69.5 ms (−27%)

```sql
SELECT MIN(chn.name) AS voiced_char,
       MIN(n.name) AS voicing_actress,
       MIN(t.title) AS voiced_animation
FROM aka_name AS an,
     complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     info_type AS it,
     info_type AS it3,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     name AS n,
     person_info AS pi,
     role_type AS rt,
     title AS t
WHERE cct1.kind ='cast'
  AND cct2.kind ='complete+verified'
  AND chn.name = 'Queen'
  AND ci.note IN ('(voice)',
                  '(voice) (uncredited)',
                  '(voice: English version)')
  AND cn.country_code ='[us]'
  AND it.info = 'release dates'
  AND it3.info = 'height'
  AND k.keyword = 'computer-animation'
  AND mi.info LIKE 'USA:%200%'
  AND n.gender ='f'
  AND n.name LIKE '%An%'
  AND rt.role ='actress'
  AND t.title = 'Shrek 2'
  AND t.production_year BETWEEN 2000 AND 2005
  AND t.id = mi.movie_id
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = cc.movie_id
  AND mc.movie_id = ci.movie_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mk.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = ci.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = cc.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND cn.id = mc.company_id
  AND it.id = mi.info_type_id
  AND n.id = ci.person_id
  AND rt.id = ci.role_id
  AND n.id = an.person_id
  AND ci.person_id = an.person_id
  AND chn.id = ci.person_role_id
  AND n.id = pi.person_id
  AND ci.person_id = pi.person_id
  AND it3.id = pi.info_type_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `631a5812` 682 ms → 147 ms (−79%)

```sql
SELECT MIN(chn.name) AS voiced_char,
       MIN(n.name) AS voicing_actress,
       MIN(t.title) AS voiced_animation
FROM aka_name AS an,
     complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     info_type AS it,
     info_type AS it3,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     name AS n,
     person_info AS pi,
     role_type AS rt,
     title AS t
WHERE cct1.kind ='cast'
  AND cct2.kind ='complete+verified'
  AND ci.note IN ('(voice)',
                  '(voice: Japanese version)',
                  '(voice) (uncredited)',
                  '(voice: English version)')
  AND cn.country_code ='[us]'
  AND it.info = 'release dates'
  AND it3.info = 'trivia'
  AND k.keyword = 'computer-animation'
  AND mi.info IS NOT NULL
  AND (mi.info LIKE 'Japan:%200%'
       OR mi.info LIKE 'USA:%200%')
  AND n.gender ='f'
  AND n.name LIKE '%An%'
  AND rt.role ='actress'
  AND t.production_year BETWEEN 2000 AND 2010
  AND t.id = mi.movie_id
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = cc.movie_id
  AND mc.movie_id = ci.movie_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mk.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = ci.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = cc.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND cn.id = mc.company_id
  AND it.id = mi.info_type_id
  AND n.id = ci.person_id
  AND rt.id = ci.role_id
  AND n.id = an.person_id
  AND ci.person_id = an.person_id
  AND chn.id = ci.person_role_id
  AND n.id = pi.person_id
  AND ci.person_id = pi.person_id
  AND it3.id = pi.info_type_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `c213f9bf` 3,286 ms → 653 ms (−80%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS complete_violent_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     cast_info AS ci,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind ='complete+verified'
  AND ci.note IN ('(writer)',
                  '(head writer)',
                  '(written by)',
                  '(story)',
                  '(story editor)')
  AND it1.info = 'genres'
  AND it2.info = 'votes'
  AND k.keyword IN ('murder',
                    'violence',
                    'blood',
                    'gore',
                    'death',
                    'female-nudity',
                    'hospital')
  AND mi.info IN ('Horror',
                  'Thriller')
  AND n.gender = 'm'
  AND t.production_year > 2000
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = cc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = cc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = cc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no bitmap scan, 1 call
- **Plan:** Bitmap Heap Scan on movie_keyword → Index Scan on movie_keyword

```sql
SET LOCAL enable_bitmapscan = off;
```

### `311123fe` 138 ms → 94.1 ms (−32%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS violent_liongate_movie
FROM cast_info AS ci,
     company_name AS cn,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE ci.note IN ('(writer)',
                  '(head writer)',
                  '(written by)',
                  '(story)',
                  '(story editor)')
  AND cn.name LIKE 'Lionsgate%'
  AND it1.info = 'genres'
  AND it2.info = 'votes'
  AND k.keyword IN ('murder',
                    'violence',
                    'blood',
                    'gore',
                    'death',
                    'female-nudity',
                    'hospital')
  AND mc.note LIKE '%(Blu-ray)%'
  AND mi.info IN ('Horror',
                  'Thriller')
  AND n.gender = 'm'
  AND t.production_year > 2000
  AND (t.title LIKE '%Freddy%'
       OR t.title LIKE '%Jason%'
       OR t.title LIKE 'Saw%')
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = mc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cn.id = mc.company_id
```

- **Hint:** row counts, 1 call
- **Plan:** Nested Loop, Index Scan on info_type → Hash Join, Memoize, Seq Scan on info_type

```sql
/*+ Rows(cn mc *68) Rows(cn mc mi_idx *100) Rows(cn it2 mc mi_idx *67) Rows(cn it2 mc mi_idx t *7) Rows(cn it2 mc mi_idx mk t *890) Rows(ci cn it2 mc mi_idx mk t *1600) Rows(ci cn it2 mc mi mi_idx mk t *2700) Rows(ci cn it1 it2 mc mi mi_idx mk t *2700) Rows(ci cn it1 it2 k mc mi mi_idx mk t *84) Rows(ci cn it1 it2 k mc mi mi_idx mk n t *84) */ -- at the start of the query, with pg_hint_plan
```

### `ddcd52ba` 89.8 ms → 58.2 ms (−35%)

```sql
SELECT MIN(cn1.name) AS first_company,
       MIN(cn2.name) AS second_company,
       MIN(mi_idx1.info) AS first_rating,
       MIN(mi_idx2.info) AS second_rating,
       MIN(t1.title) AS first_movie,
       MIN(t2.title) AS second_movie
FROM company_name AS cn1,
     company_name AS cn2,
     info_type AS it1,
     info_type AS it2,
     kind_type AS kt1,
     kind_type AS kt2,
     link_type AS lt,
     movie_companies AS mc1,
     movie_companies AS mc2,
     movie_info_idx AS mi_idx1,
     movie_info_idx AS mi_idx2,
     movie_link AS ml,
     title AS t1,
     title AS t2
WHERE cn1.country_code = '[us]'
  AND it1.info = 'rating'
  AND it2.info = 'rating'
  AND kt1.kind IN ('tv series')
  AND kt2.kind IN ('tv series')
  AND lt.link IN ('sequel',
                  'follows',
                  'followed by')
  AND mi_idx2.info < '3.0'
  AND t2.production_year BETWEEN 2005 AND 2008
  AND lt.id = ml.link_type_id
  AND t1.id = ml.movie_id
  AND t2.id = ml.linked_movie_id
  AND it1.id = mi_idx1.info_type_id
  AND t1.id = mi_idx1.movie_id
  AND kt1.id = t1.kind_id
  AND cn1.id = mc1.company_id
  AND t1.id = mc1.movie_id
  AND ml.movie_id = mi_idx1.movie_id
  AND ml.movie_id = mc1.movie_id
  AND mi_idx1.movie_id = mc1.movie_id
  AND it2.id = mi_idx2.info_type_id
  AND t2.id = mi_idx2.movie_id
  AND kt2.id = t2.kind_id
  AND cn2.id = mc2.company_id
  AND t2.id = mc2.movie_id
  AND ml.linked_movie_id = mi_idx2.movie_id
  AND ml.linked_movie_id = mc2.movie_id
  AND mi_idx2.movie_id = mc2.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×3, Merge Join, Seq Scan on movie_link, Seq Scan on kind_type → Nested Loop ×4, Index Scan on kind_type, Bitmap Heap Scan on movie_link

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `6744415b` 81.9 ms → 51.4 ms (−37%)

```sql
SELECT MIN(cn1.name) AS first_company,
       MIN(cn2.name) AS second_company,
       MIN(mi_idx1.info) AS first_rating,
       MIN(mi_idx2.info) AS second_rating,
       MIN(t1.title) AS first_movie,
       MIN(t2.title) AS second_movie
FROM company_name AS cn1,
     company_name AS cn2,
     info_type AS it1,
     info_type AS it2,
     kind_type AS kt1,
     kind_type AS kt2,
     link_type AS lt,
     movie_companies AS mc1,
     movie_companies AS mc2,
     movie_info_idx AS mi_idx1,
     movie_info_idx AS mi_idx2,
     movie_link AS ml,
     title AS t1,
     title AS t2
WHERE cn1.country_code = '[nl]'
  AND it1.info = 'rating'
  AND it2.info = 'rating'
  AND kt1.kind IN ('tv series')
  AND kt2.kind IN ('tv series')
  AND lt.link LIKE '%follow%'
  AND mi_idx2.info < '3.0'
  AND t2.production_year = 2007
  AND lt.id = ml.link_type_id
  AND t1.id = ml.movie_id
  AND t2.id = ml.linked_movie_id
  AND it1.id = mi_idx1.info_type_id
  AND t1.id = mi_idx1.movie_id
  AND kt1.id = t1.kind_id
  AND cn1.id = mc1.company_id
  AND t1.id = mc1.movie_id
  AND ml.movie_id = mi_idx1.movie_id
  AND ml.movie_id = mc1.movie_id
  AND mi_idx1.movie_id = mc1.movie_id
  AND it2.id = mi_idx2.info_type_id
  AND t2.id = mi_idx2.movie_id
  AND kt2.id = t2.kind_id
  AND cn2.id = mc2.company_id
  AND t2.id = mc2.movie_id
  AND ml.linked_movie_id = mi_idx2.movie_id
  AND ml.linked_movie_id = mc2.movie_id
  AND mi_idx2.movie_id = mc2.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×2, Merge Join → Nested Loop ×3, Memoize

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `0da43d7b` 87.7 ms → 66.0 ms (−25%)

```sql
SELECT MIN(cn1.name) AS first_company,
       MIN(cn2.name) AS second_company,
       MIN(mi_idx1.info) AS first_rating,
       MIN(mi_idx2.info) AS second_rating,
       MIN(t1.title) AS first_movie,
       MIN(t2.title) AS second_movie
FROM company_name AS cn1,
     company_name AS cn2,
     info_type AS it1,
     info_type AS it2,
     kind_type AS kt1,
     kind_type AS kt2,
     link_type AS lt,
     movie_companies AS mc1,
     movie_companies AS mc2,
     movie_info_idx AS mi_idx1,
     movie_info_idx AS mi_idx2,
     movie_link AS ml,
     title AS t1,
     title AS t2
WHERE cn1.country_code != '[us]'
  AND it1.info = 'rating'
  AND it2.info = 'rating'
  AND kt1.kind IN ('tv series',
                   'episode')
  AND kt2.kind IN ('tv series',
                   'episode')
  AND lt.link IN ('sequel',
                  'follows',
                  'followed by')
  AND mi_idx2.info < '3.5'
  AND t2.production_year BETWEEN 2000 AND 2010
  AND lt.id = ml.link_type_id
  AND t1.id = ml.movie_id
  AND t2.id = ml.linked_movie_id
  AND it1.id = mi_idx1.info_type_id
  AND t1.id = mi_idx1.movie_id
  AND kt1.id = t1.kind_id
  AND cn1.id = mc1.company_id
  AND t1.id = mc1.movie_id
  AND ml.movie_id = mi_idx1.movie_id
  AND ml.movie_id = mc1.movie_id
  AND mi_idx1.movie_id = mc1.movie_id
  AND it2.id = mi_idx2.info_type_id
  AND t2.id = mi_idx2.movie_id
  AND kt2.id = t2.kind_id
  AND cn2.id = mc2.company_id
  AND t2.id = mc2.movie_id
  AND ml.linked_movie_id = mi_idx2.movie_id
  AND ml.linked_movie_id = mc2.movie_id
  AND mi_idx2.movie_id = mc2.movie_id
```

- **Hint:** no merge join, 1 call
- **Plan:** Hash Join, Merge Join, Index Scan on kind_type ×2 → Nested Loop ×2, Seq Scan on kind_type ×2

```sql
SET LOCAL enable_mergejoin = off;
```

## Keep the default plan (51)

| Shape | Query | Time | Why |
|---|---|---|---|
| `653a69e1` | SELECT MIN(t.title) AS movie_title FROM keyword AS k, movie… | 100.0 ms | no other plan was clearly faster |
| `bcfea86c` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS movie_ti… | 67.4 ms | no other plan was clearly faster |
| `fbf68f9d` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 9.37 ms | no other plan was clearly faster |
| `e5a4de69` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 550 ms | no other plan was clearly faster |
| `27998f6b` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 1,488 ms | a hint helped one sample but was slower on another |
| `0ea80b8e` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 449 ms | no other plan was clearly faster |
| `11d2f26e` | SELECT MIN(cn.name) AS company_name, MIN(lt.link) AS link_t… | 58.8 ms | no other plan was clearly faster |
| `3564c7a4` | SELECT MIN(lt.link) AS link_type, MIN(t1.title) AS first_mo… | 38.5 ms | no other plan was clearly faster |
| `8f7394ec` | SELECT MIN(t.title) AS typical_european_movie FROM company_… | 142 ms | no other plan was clearly faster |
| `c3f27144` | SELECT MIN(t.title) AS american_vhs_movie FROM company_type… | 135 ms | no other plan was clearly faster |
| `c936eca9` | SELECT MIN(t.title) AS american_movie FROM company_type AS … | 173 ms | no other plan was clearly faster |
| `3d48869d` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 2,895 ms | no other plan was clearly faster |
| `376e1e8f` | SELECT MIN(an1.name) AS actress_pseudonym, MIN(t.title) AS … | 718 ms | no other plan was clearly faster |
| `5d5082e3` | SELECT MIN(an.name) AS acress_pseudonym, MIN(t.title) AS ja… | 163 ms | no other plan was clearly faster |
| `5671211f` | SELECT MIN(a1.name) AS writer_pseudo_name, MIN(t.title) AS … | 2,969 ms | no other plan was clearly faster |
| `e80417f5` | SELECT MIN(an1.name) AS costume_designer_pseudo, MIN(t.titl… | 1,057 ms | no other plan was clearly faster |
| `4a51ac6c` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS c… | 264 ms | no other plan was clearly faster |
| `2a882a48` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 261 ms | no other plan was clearly faster |
| `dd477274` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 422 ms | no other plan was clearly faster |
| `39b4cff4` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 2,436 ms | no other plan was clearly faster |
| `d468dc0f` | SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_… | 23.6 ms | no other plan was clearly faster |
| `46c94c7f` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 172 ms | no other plan was clearly faster |
| `fc50c266` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 486 ms | no other plan was clearly faster |
| `06df28fb` | SELECT MIN(cn.name) AS producing_company, MIN(miidx.info) A… | 1,549 ms | no other plan was clearly faster |
| `ccc29962` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS northern… | 162 ms | no other plan was clearly faster |
| `e14ae77f` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS western_… | 68.4 ms | no other plan was clearly faster |
| `c56fadd6` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS north_eu… | 330 ms | no other plan was clearly faster |
| `ef249dc4` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS intern… | 260 ms | no other plan was clearly faster |
| `407c64ff` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS youtub… | 32.7 ms | no other plan was clearly faster |
| `a05671a4` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS modern… | 364 ms | no other plan was clearly faster |
| `5cce4f36` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 970 ms | no other plan was clearly faster |
| `43101882` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 167 ms | no other plan was clearly faster |
| `86f7e26b` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 2,937 ms | no other plan was clearly faster |
| `e27f8ea2` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS voic… | 283 ms | no other plan was clearly faster |
| `2ec85d5d` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS kung… | 164 ms | no other plan was clearly faster |
| `fa4f3af1` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS jap_… | 431 ms | no other plan was clearly faster |
| `8ed005c6` | SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c… | 1,440 ms | no other plan was clearly faster |
| `e61f5cbd` | SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c… | 1,195 ms | no other plan was clearly faster |
| `c9d70fe1` | SELECT MIN(n.name) AS cast_member, MIN(t.title) AS complete… | 621 ms | no other plan was clearly faster |
| `45290f03` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 550 ms | no other plan was clearly faster |
| `3f5cfdf1` | SELECT MIN(kt.kind) AS movie_kind, MIN(t.title) AS complete… | 60.7 ms | no other plan was clearly faster |
| `c335bb2c` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 1,636 ms | no other plan was clearly faster |
| `0466dcec` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 209 ms | no other plan was clearly faster |
| `135e5bc8` | SELECT MIN(chn.name) AS character_name, MIN(mi_idx.info) AS… | 113 ms | no other plan was clearly faster |
| `7483ed43` | SELECT MIN(chn.name) AS character_name, MIN(mi_idx.info) AS… | 1,665 ms | no other plan was clearly faster |
| `4e8b8a70` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 264 ms | no other plan was clearly faster |
| `ff8164ee` | SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing… | 97.4 ms | no other plan was clearly faster |
| `929552c1` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 309 ms | no other plan was clearly faster |
| `2b1949c5` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 1,719 ms | no other plan was clearly faster |
| `376fabaf` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 204 ms | no other plan was clearly faster |
| `442faf79` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 299 ms | no other plan was clearly faster |

Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of 3 runs per plan.
