"""Source UTF-8 preflight and directory/clock/mask ordering of public spill."""
import copy
import json
import tempfile
from pathlib import Path
from unittest.mock import patch


def public_budget_text_contracts() -> dict:
    import mini_loop.compaction as module
    base = dict(ids=[['id']], texts=[['plain text']], maximum=0, preview=2)
    recipes = [dict(name='plain/default', maximum=200000),dict(name='plain/spill'),
               dict(name='unicode/positive',texts=[['界😀e\u0301']],preview=3),
               dict(name='unicode/negative',texts=[['界😀e\u0301']],preview=-1),
               dict(name='unicode/budget-exact',texts=[['界😀e\u0301']],maximum=10),
               dict(name='id/empty',ids=[['']]),dict(name='id/unicode',ids=[['id界😀tail']]),
               dict(name='id/high-surrogate',ids=[['start','\ud800','end']]),
               dict(name='id/surrogate-pair-literal',ids=[['start','\ud83d','\ude00','end']]),
               dict(name='id/character-cap',ids=[['A'*98,'\ud800','😀','tail']]),
               dict(name='older-invalid-ignored',older=True),
               dict(name='directory-blocked',blocked=True),
               dict(name='mask-fault-first',mask='raise-first'),
               dict(name='mask-fault-second',ids=[['first'],['second']],texts=[['first text'],['second text']],mask='raise-second'),
               dict(name='mask-invalid-first',mask='invalid-first'),
               dict(name='mask-invalid-second',ids=[['first'],['second']],texts=[['first text'],['second text']],mask='invalid-second')]
    for label,parts in [('high',['\ud800']),('low',['\udfff']),('literal-pair',['\ud83d','\ude00']),('later',["safe"] )]:
        for maximum in [0,200000]:
            row=dict(name=f'text/{label}/{maximum}',maximum=maximum,mask='replace')
            if label=='later':row.update(ids=[['first'],['second']],texts=[['safe'],['\ud800']])
            else:row['texts']=[parts]
            recipes.append(row)
    recipes.append(dict(name='mask-can-not-rescue-raw',texts=[['prefix','\ud800','suffix']],mask='replace'))

    def capture(recipe, root):
        row={**base,**recipe};root.mkdir(parents=True)
        output=root/'.task_outputs/tool-results'
        if row.get('blocked'):
            output.parent.mkdir();output.write_text('blocking file')
        messages=[dict(role='user',content=[dict(type='tool_result',tool_use_id=''.join(parts),content=''.join(text),is_error=False)
                                           for parts,text in zip(row['ids'],row['texts'])])]
        if row.get('older'):
            messages=[dict(role='user',content=[dict(type='tool_result',tool_use_id='older',content='\ud800')])]+messages
            row['maximum']=200000
        before=copy.deepcopy(messages);original_message=messages[-1];trace=[];dir_at_clock=[]
        class Masker:
            def __init__(self):self.calls=0
            def mask(self,text):
                self.calls+=1;trace.append('mask')
                mode=row.get('mask','passthrough')
                if mode==f'raise-{["first","second"][min(self.calls-1,1)]}':
                    raise RuntimeError('mask callback fault')
                if mode==f'invalid-{["first","second"][min(self.calls-1,1)]}':
                    return '\ud800'
                return 'masked' if mode=='replace' else text
        mask=Masker()
        def clock():trace.append('clock');dir_at_clock.append(output.is_dir());return 123.456
        count,error=0,None
        with patch.object(module.time,'time',clock):
            try:count=module.tool_result_budget(messages,root,max_bytes=row['maximum'],preview_chars=row['preview'],secrets=mask)
            except Exception as exc:error=dict(kind=type(exc).__name__,message=str(exc).replace(str(root),'<workspace>'))
        normalize=lambda s:s.replace(str(root),'<workspace>')
        files={p.name:p.read_text() for p in sorted(output.glob('*.txt')) if p.is_file()}
        # JSON pieces retain adjacent literal surrogate code points individually.
        parts=lambda values:[[json.dumps(piece,ensure_ascii=True) for piece in pieces] for pieces in values]
        return dict(name=row['name'],ids=parts(row['ids']),texts=parts(row['texts']),maximum=row['maximum'],preview=row['preview'],
                    older=row.get('older',False),blocked=row.get('blocked',False),mask=row.get('mask','passthrough'),
                    error=error,count=count,trace=trace,directory_at_clock=dir_at_clock,output_directory=output.is_dir(),
                    files=files,message_replaced=messages[-1] is not original_message,
                    after_content_json=[[json.dumps(normalize(part['content']),ensure_ascii=True)] if messages[-1] is not original_message
                                        else [json.dumps(piece,ensure_ascii=True) for piece in row['texts'][i]]
                                        for i,part in enumerate(messages[-1]['content'])],
                    before_unchanged_on_error=error is None or messages==before)
    with tempfile.TemporaryDirectory(prefix='mini-loop-public-budget-text-') as scratch:
        cases=[capture(row,Path(scratch)/str(i)) for i,row in enumerate(recipes)]
    return dict(cases=cases,limits=['Actual public Source tool_result_budget; original Unicode encode check precedes budget, directory, clock and mask.',
                                  'Explicit typed native workspace guards precede directory writes; no owner/model authority is granted.',
                                  'Surrogate IDs are sanitized by Python code points, then capped at100 characters.',
                                  'Adjacent literal surrogate pieces remain separate observations; JSON pair normalization is not substituted.',
                                  'Filesystem roots, arbitrary umasks/callbacks and broader public/profile/platform acceptance remain open.'])
