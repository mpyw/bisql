select * /** all columns */ from t
/*%! sorting */ /*%if sorts != null*/order by /*%for s in sorts*//*# s */ /*# s_next_comma */ /*%end*//*%end*/
