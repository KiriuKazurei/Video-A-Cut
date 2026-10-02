"""All three native formats through fixed profiles, real sampling/SAPI and export."""
import sys
from verify_phase5_workflow import main

if __name__ == '__main__':
    try:
        for format in ('openai', 'anthropic', 'gemini'):
            print('Testing provider format:', format, flush=True)
            if main(prepared=True, api_format=format, content_only='--content-only' in sys.argv):
                sys.exit(1)
    except Exception as error:
        import traceback
        traceback.print_exc()
        print(f'FAIL: {error}', file=sys.stderr)
        sys.exit(1)
