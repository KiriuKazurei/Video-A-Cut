"""Isolated prepared-profile end-to-end run; no human acceptance is recorded."""
import sys
from verify_phase5_workflow import main
if __name__=='__main__':
    try:sys.exit(main(prepared=True))
    except Exception as error:print(f'FAIL: {error}',file=sys.stderr);sys.exit(1)
