DELETE FROM public.platform_config WHERE config_key IN ('assistant_enabled', 'assistant_daily_questions');
DROP TABLE IF EXISTS public.assistant_question_days;
