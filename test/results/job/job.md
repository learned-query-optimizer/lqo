# LQO advisor report

- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2), database imdb (primary, allowed with --allow-primary)
- **Workload:** job.sql (sql): 113 statements, 94 shapes
- **Settings:** standard hints (13), 3 timed runs after a warm-up, up to 10 samples per shape, a hint must save 20%
- **Estimate:** one pass of the tested statements takes 172,560 ms now, 77,636 ms with these hints (−55%)

## Faster with a hint (35 of 94 tested)

### `a801be56` 3,728 ms → 2,648 ms (−29%)

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

### `73f27e86` 4,631 ms → 2,775 ms (−40%)

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

- **Hint:** no nested loop, 2 calls
- **Plan:** Nested Loop ×6, Bitmap Heap Scan on movie_keyword, Index Scan on cast_info, Index Scan on name, Index Scan on movie_companies, Index Only Scan on company_name, Index Only Scan on title → Hash Join ×6, Seq Scan on title, Seq Scan on company_name, Seq Scan on movie_companies, Seq Scan on cast_info, Seq Scan on name, Seq Scan on movie_keyword

```sql
SET LOCAL enable_nestloop = off;
```

### `6d1d5dbb` 261 ms → 183 ms (−30%)

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

### `0cf295d8` 75.8 ms → 4.41 ms (−94%)

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

- **Hint:** no sequential scan, 1 call
- **Plan:** Hash Join ×2, Seq Scan on movie_info_idx, Seq Scan on info_type, Seq Scan on company_type → Nested Loop ×2, Index Scan on info_type, Bitmap Heap Scan on movie_info_idx, Memoize, Index Scan on company_type

```sql
SET LOCAL enable_seqscan = off;
```

### `078dfa3f` 69.6 ms → 0.67 ms (−99%)

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

- **Hint:** no hash join, 1 call
- **Plan:** Hash Join ×2, Seq Scan on movie_info_idx → Nested Loop ×2, Bitmap Heap Scan on movie_info_idx

```sql
SET LOCAL enable_hashjoin = off;
```

### `ae99dc85` 74.5 ms → 3.30 ms (−96%)

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

- **Hint:** no sequential scan, 1 call
- **Plan:** Hash Join ×2, Seq Scan on movie_info_idx, Seq Scan on info_type, Seq Scan on company_type → Nested Loop ×2, Index Scan on info_type, Bitmap Heap Scan on movie_info_idx, Memoize, Index Scan on company_type

```sql
SET LOCAL enable_seqscan = off;
```

### `450fe5f6` 69.7 ms → 0.60 ms (−99%)

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

### `b8a77e31` 2,201 ms → 1,119 ms (−49%)

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

### `5d5082e3` 159 ms → 125 ms (−21%)

```sql
SELECT MIN(an.name) AS acress_pseudonym,
       MIN(t.title) AS japanese_anime_movie
FROM aka_name AS an,
     cast_info AS ci,
     company_name AS cn,
     movie_companies AS mc,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note ='(voice: English version)'
  AND cn.country_code ='[jp]'
  AND mc.note LIKE '%(Japan)%'
  AND mc.note NOT LIKE '%(USA)%'
  AND (mc.note LIKE '%(2006)%'
       OR mc.note LIKE '%(2007)%')
  AND n.name LIKE '%Yo%'
  AND n.name NOT LIKE '%Yu%'
  AND rt.role ='actress'
  AND t.production_year BETWEEN 2006 AND 2007
  AND (t.title LIKE 'One Piece%'
       OR t.title LIKE 'Dragon Ball Z%')
  AND an.person_id = n.id
  AND n.id = ci.person_id
  AND ci.movie_id = t.id
  AND t.id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.role_id = rt.id
  AND an.person_id = ci.person_id
  AND ci.movie_id = mc.movie_id
```

- **Hint:** no sequential scan, 1 call
- **Plan:** Seq Scan on movie_companies, Memoize → Index Scan on movie_companies

```sql
SET LOCAL enable_seqscan = off;
```

### `dd477274` 1,409 ms → 448 ms (−68%)

```sql
SELECT MIN(an.name) AS alternative_name,
       MIN(chn.name) AS voiced_character_name,
       MIN(n.name) AS voicing_actress,
       MIN(t.title) AS american_movie
FROM aka_name AS an,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     movie_companies AS mc,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note IN ('(voice)',
                  '(voice: Japanese version)',
                  '(voice) (uncredited)',
                  '(voice: English version)')
  AND cn.country_code ='[us]'
  AND n.gender ='f'
  AND n.name LIKE '%An%'
  AND rt.role ='actress'
  AND ci.movie_id = t.id
  AND t.id = mc.movie_id
  AND ci.movie_id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.role_id = rt.id
  AND n.id = ci.person_id
  AND chn.id = ci.person_role_id
  AND an.person_id = n.id
  AND an.person_id = ci.person_id
```

- **Hint:** no index scan, 1 call
- **Plan:** Nested Loop ×3, Index Scan on cast_info, Memoize, Index Scan on char_name, Index Scan on name, Index Scan on movie_companies, Index Scan on company_name, Index Scan on title, Index Scan on aka_name → Hash Join ×3, Seq Scan on aka_name, Seq Scan on name, Bitmap Heap Scan on cast_info, Bitmap Heap Scan on char_name, Bitmap Heap Scan on movie_companies, Seq Scan on company_name, Bitmap Heap Scan on title

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

### `70b0406f` 294 ms → 200 ms (−32%)

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

### `6eab06ef` 211 ms → 95.2 ms (−55%)

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

### `850e1a80` 1,004 ms → 105 ms (−90%)

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

### `8461866c` 167 ms → 90.6 ms (−46%)

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

### `729ccb90` 76.3 ms → 2.85 ms (−96%)

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

### `8f30d4e5` 11,304 ms → 4,369 ms (−61%)

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

- **Hint:** no nested loop, 1 call
- **Plan:** Nested Loop ×7, Bitmap Heap Scan on movie_keyword, Index Scan on title, Index Scan on movie_companies, Index Scan on company_name, Index Scan on cast_info, Index Only Scan on name, Index Scan on aka_name → Hash Join ×7, Seq Scan on name, Seq Scan on aka_name, Seq Scan on cast_info, Seq Scan on company_name, Seq Scan on movie_companies, Seq Scan on title, Seq Scan on movie_keyword

```sql
SET LOCAL enable_nestloop = off;
```

### `6d17eb01` 7,238 ms → 2,773 ms (−62%)

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

- **Hint:** no nested loop, 1 call
- **Plan:** Nested Loop ×6, Bitmap Heap Scan on movie_keyword, Index Only Scan on title, Index Scan on movie_companies, Index Scan on company_name, Index Scan on cast_info, Index Scan on name → Hash Join ×6, Seq Scan on cast_info, Seq Scan on name, Seq Scan on company_name, Seq Scan on movie_companies, Seq Scan on title, Seq Scan on movie_keyword

```sql
SET LOCAL enable_nestloop = off;
```

### `85cf65fe` 7,106 ms → 3,329 ms (−53%)

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

- **Hint:** no nested loop, 1 call
- **Plan:** Nested Loop ×6, Bitmap Heap Scan on movie_keyword, Index Only Scan on title, Index Scan on movie_companies, Index Scan on company_name, Index Scan on cast_info, Index Scan on name → Hash Join ×6, Seq Scan on cast_info, Seq Scan on company_name, Seq Scan on movie_companies, Seq Scan on title, Seq Scan on movie_keyword, Seq Scan on name

```sql
SET LOCAL enable_nestloop = off;
```

### `fa4f3af1` 1,334 ms → 485 ms (−64%)

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
  AND mi.info IS NOT NULL
  AND (mi.info LIKE 'Japan:%200%'
       OR mi.info LIKE 'USA:%200%')
  AND n.gender ='f'
  AND n.name LIKE '%An%'
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

- **Hint:** no index scan, 1 call
- **Plan:** Nested Loop ×3, Index Scan on cast_info, Memoize, Index Only Scan on char_name, Index Scan on name, Index Scan on title, Index Scan on movie_info, Index Scan on movie_companies, Index Scan on company_name, Index Only Scan on aka_name → Hash Join ×3, Seq Scan on aka_name, Seq Scan on name, Bitmap Heap Scan on cast_info, Bitmap Heap Scan on movie_info, Bitmap Heap Scan on char_name, Bitmap Heap Scan on title, Bitmap Heap Scan on movie_companies, Bitmap Heap Scan on company_name

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

### `8cf8918f` 8,471 ms → 3,006 ms (−65%)

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

### `6ef26656` 84.4 ms → 59.3 ms (−30%)

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
- **Plan:** Index Scan on info_type, Index Scan on role_type → Seq Scan on info_type, Seq Scan on role_type

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `2066cfd6` 968 ms → 502 ms (−48%)

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
- **Plan:** Hash Join ×2, Seq Scan on complete_cast, Index Scan on movie_info_idx, Index Scan on title, Index Scan on cast_info, Index Scan on char_name, Index Scan on name, Index Scan on movie_keyword, Index Scan on keyword → Nested Loop ×2, Seq Scan on keyword, Bitmap Heap Scan on movie_keyword, Bitmap Heap Scan on movie_info_idx, Bitmap Heap Scan on complete_cast, Bitmap Heap Scan on title, Bitmap Heap Scan on cast_info, Bitmap Heap Scan on char_name, Bitmap Heap Scan on name

```sql
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
```

### `7483ed43` 59,196 ms → 1,211 ms (−98%)

```sql
SELECT MIN(chn.name) AS character_name,
       MIN(mi_idx.info) AS rating,
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

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `188e45da` 71.5 ms → 55.0 ms (−23%)

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

### `4a1dc513` 62.1 ms → 44.4 ms (−28%)

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

### `c862384f` 81.8 ms → 53.9 ms (−34%)

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

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×3, Merge Join → Nested Loop ×4

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `b26e54ae` 538 ms → 308 ms (−43%)

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

- **Hint:** no sequential scan, 1 call
- **Plan:** Seq Scan on info_type ×2, Seq Scan on comp_cast_type ×2, Seq Scan on kind_type, Seq Scan on keyword, Seq Scan on company_type → Index Scan on keyword, Memoize, Index Scan on kind_type, Index Scan on comp_cast_type ×2, Index Scan on info_type ×2, Index Only Scan on company_type

```sql
SET LOCAL enable_seqscan = off;
```

### `e4943b34` 332 ms → 236 ms (−29%)

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

- **Hint:** no bitmap scan, 1 call
- **Plan:** Bitmap Heap Scan on movie_keyword → Index Scan on movie_keyword

```sql
SET LOCAL enable_bitmapscan = off;
```

### `4e8b8a70` 460 ms → 240 ms (−48%)

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
WHERE cct1.kind = 'cast'
  AND cct2.kind = 'complete'
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

### `631a5812` 1,081 ms → 161 ms (−85%)

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

- **Hint:** no hash join, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL enable_hashjoin = off;
```

### `c213f9bf` 3,313 ms → 553 ms (−83%)

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

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `2b1949c5` 2,547 ms → 1,606 ms (−37%)

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
WHERE cct1.kind = 'cast'
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

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `ddcd52ba` 89.2 ms → 52.2 ms (−41%)

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
- **Plan:** Index Scan on kind_type, Hash Join ×2, Merge Join, Seq Scan on movie_link → Nested Loop ×3, Seq Scan on kind_type, Bitmap Heap Scan on movie_link

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `6744415b` 83.7 ms → 54.9 ms (−34%)

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
- **Plan:** Hash Join ×2, Merge Join → Nested Loop ×3

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `0da43d7b` 88.9 ms → 69.4 ms (−22%)

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

- **Hint:** no parallel workers, 1 call
- **Plan:** Seq Scan on info_type → Index Scan on info_type

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

## Keep the default plan (59)

| Shape | Query | Time | Why |
|---|---|---|---|
| `dd1b70c1` | SELECT MIN(t.title) AS movie_title FROM company_name AS cn,… | 346 ms | no other plan was clearly faster |
| `653a69e1` | SELECT MIN(t.title) AS movie_title FROM keyword AS k, movie… | 97.3 ms | no other plan was clearly faster |
| `bcfea86c` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS movie_ti… | 53.6 ms | no other plan was clearly faster |
| `fbf68f9d` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 8.85 ms | no other plan was clearly faster |
| `e5a4de69` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 544 ms | no other plan was clearly faster |
| `27998f6b` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 1,439 ms | no other plan was clearly faster |
| `718b520c` | SELECT MIN(cn.name) AS producing_company, MIN(miidx.info) A… | 446 ms | no other plan was clearly faster |
| `0ea80b8e` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 443 ms | no other plan was clearly faster |
| `11d2f26e` | SELECT MIN(cn.name) AS company_name, MIN(lt.link) AS link_t… | 57.0 ms | no other plan was clearly faster |
| `3564c7a4` | SELECT MIN(lt.link) AS link_type, MIN(t1.title) AS first_mo… | 38.3 ms | no other plan was clearly faster |
| `8f7394ec` | SELECT MIN(t.title) AS typical_european_movie FROM company_… | 133 ms | no other plan was clearly faster |
| `c3f27144` | SELECT MIN(t.title) AS american_vhs_movie FROM company_type… | 125 ms | no other plan was clearly faster |
| `c936eca9` | SELECT MIN(t.title) AS american_movie FROM company_type AS … | 170 ms | no other plan was clearly faster |
| `3d48869d` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 2,732 ms | no other plan was clearly faster |
| `cc66d5be` | SELECT MIN(n.name) AS of_person, MIN(t.title) AS biography_… | 478 ms | no other plan was clearly faster |
| `46b42be8` | SELECT MIN(n.name) AS of_person, MIN(t.title) AS biography_… | 115 ms | no other plan was clearly faster |
| `376e1e8f` | SELECT MIN(an1.name) AS actress_pseudonym, MIN(t.title) AS … | 703 ms | no other plan was clearly faster |
| `5671211f` | SELECT MIN(a1.name) AS writer_pseudo_name, MIN(t.title) AS … | 2,949 ms | no other plan was clearly faster |
| `e80417f5` | SELECT MIN(an1.name) AS costume_designer_pseudo, MIN(t.titl… | 1,069 ms | no other plan was clearly faster |
| `4a51ac6c` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS c… | 265 ms | no other plan was clearly faster |
| `2a882a48` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 275 ms | no other plan was clearly faster |
| `39b4cff4` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 2,462 ms | no other plan was clearly faster |
| `534b68fc` | SELECT MIN(chn.name) AS character, MIN(t.title) AS movie_wi… | 2,183 ms | no other plan was clearly faster |
| `c3eba7e4` | SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_… | 45.3 ms | no other plan was clearly faster |
| `d468dc0f` | SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_… | 22.8 ms | no other plan was clearly faster |
| `46c94c7f` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 178 ms | no other plan was clearly faster |
| `fc50c266` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 487 ms | no other plan was clearly faster |
| `91a61e77` | SELECT MIN(mi.info) AS release_date, MIN(miidx.info) AS rat… | 1,012 ms | no other plan was clearly faster |
| `06df28fb` | SELECT MIN(cn.name) AS producing_company, MIN(miidx.info) A… | 1,550 ms | no other plan was clearly faster |
| `ccc29962` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS northern… | 167 ms | no other plan was clearly faster |
| `e14ae77f` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS western_… | 67.1 ms | no other plan was clearly faster |
| `c56fadd6` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS north_eu… | 329 ms | no other plan was clearly faster |
| `ef249dc4` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS intern… | 261 ms | no other plan was clearly faster |
| `407c64ff` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS youtub… | 32.7 ms | no other plan was clearly faster |
| `a05671a4` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS modern… | 331 ms | no other plan was clearly faster |
| `5798935c` | SELECT MIN(at.title) AS aka_title, MIN(t.title) AS internet… | 355 ms | no other plan was clearly faster |
| `5cce4f36` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 967 ms | no other plan was clearly faster |
| `2656274c` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 2,246 ms | no other plan was clearly faster |
| `43101882` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 161 ms | no other plan was clearly faster |
| `86f7e26b` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 2,901 ms | no other plan was clearly faster |
| `e27f8ea2` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS voic… | 290 ms | no other plan was clearly faster |
| `2ec85d5d` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS kung… | 152 ms | no other plan was clearly faster |
| `8ed005c6` | SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c… | 1,414 ms | no other plan was clearly faster |
| `e61f5cbd` | SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c… | 1,162 ms | no other plan was clearly faster |
| `c9d70fe1` | SELECT MIN(n.name) AS cast_member, MIN(t.title) AS complete… | 597 ms | no other plan was clearly faster |
| `0cc52afa` | SELECT MIN(cn.name) AS company_name, MIN(lt.link) AS link_t… | 52.2 ms | no other plan was clearly faster |
| `45290f03` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 547 ms | no other plan was clearly faster |
| `3f5cfdf1` | SELECT MIN(kt.kind) AS movie_kind, MIN(t.title) AS complete… | 59.3 ms | no other plan was clearly faster |
| `39172b8c` | SELECT MIN(chn.name) AS voiced_char_name, MIN(n.name) AS vo… | 155 ms | no other plan was clearly faster |
| `c335bb2c` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 1,606 ms | no other plan was clearly faster |
| `0466dcec` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 202 ms | no other plan was clearly faster |
| `92588c28` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 4,460 ms | no other plan was clearly faster |
| `135e5bc8` | SELECT MIN(chn.name) AS character_name, MIN(mi_idx.info) AS… | 111 ms | no other plan was clearly faster |
| `ff8164ee` | SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing… | 98.1 ms | no other plan was clearly faster |
| `05ae6e9d` | SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing… | 94.8 ms | no other plan was clearly faster |
| `929552c1` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 311 ms | no other plan was clearly faster |
| `376fabaf` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 204 ms | no other plan was clearly faster |
| `311123fe` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 133 ms | no other plan was clearly faster |
| `442faf79` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 299 ms | no other plan was clearly faster |

Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of 3 runs per plan.
