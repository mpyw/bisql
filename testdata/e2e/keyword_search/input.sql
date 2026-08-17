SELECT * FROM users
WHERE
  1 = 0
/*%for kw in keywords*/
  OR (
    name LIKE /* kw */'%sample%'
    OR email LIKE /* kw */'%sample%'
  )
/*%end*/
