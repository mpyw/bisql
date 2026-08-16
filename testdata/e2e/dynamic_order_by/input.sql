select * from t
/*%if sorts != null*/order by /*%for s in sorts*//*# s */ /*# s_next_comma */ /*%end*//*%end*/
