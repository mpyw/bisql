SELECT * FROM products
WHERE
  1 = 0
/*%for kw in keywords*/
  OR (
    x LIKE /* kw */'%sample%'
    AND y LIKE /* kw */'%sample%'
    AND z LIKE /* kw */'%sample%'
  )
/*%end*/
